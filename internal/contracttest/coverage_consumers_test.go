package contracttest_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/contracttest"
)

// TestCoverageConsumersCiteExistingEvidence resolves every Coverage.Consumer
// string to real repository evidence. Without this, a renamed or deleted test
// leaves the suite green while the registry still claims behavioral proof.
//
// A consumer is one of a small set of references, optionally combined with
// " + " or " and ":
//   - TestName: a test function in this package;
//   - internal/pkg/TestName: a test function in another package;
//   - internal/pkg/file_test.go: a test file;
//   - Type.Method or pkg.Function: a symbol in the production source.
func TestCoverageConsumersCiteExistingEvidence(t *testing.T) {
	index := newEvidenceIndex(t, moduleRoot(t))
	phases := []contracttest.Phase{
		contracttest.PhaseResolveRuntime,
		contracttest.PhaseExecute,
		contracttest.PhaseVerify,
	}
	for _, phase := range phases {
		keys := contracttest.CoverageKeys(phase)
		names := make([]string, 0, len(keys))
		for key := range keys {
			names = append(names, key)
		}
		sort.Strings(names)
		for _, key := range names {
			for _, ref := range splitConsumerRefs(keys[key].Consumer) {
				if err := index.check(ref); err != nil {
					t.Errorf("%s coverage %q cites %q: %v", phase, key, ref, err)
				}
			}
		}
	}
}

func TestCoverageConsumerCheckerRejectsGhostEvidence(t *testing.T) {
	index := newEvidenceIndex(t, moduleRoot(t))
	for _, ref := range []string{
		"TestCoverageEvidenceThatDoesNotExist",
		"internal/contracttest/TestCoverageEvidenceThatDoesNotExist",
		"internal/contracttest/does_not_exist_test.go",
		"BaseAdapter.ObserveThatDoesNotExist",
		"plan.ReconcileThatDoesNotExist",
		"NoSuchType.Observe",
		"not a reference",
	} {
		if err := index.check(ref); err == nil {
			t.Errorf("check(%q) accepted ghost evidence", ref)
		}
	}
	for _, ref := range []string{
		"TestBehavioralCoverageMatchesDeclaredFieldEffects",
		"BaseAdapter.Observe",
		"plan.Reconcile",
		"internal/git/adapter_test.go",
		"internal/ecosystem/TestAsdfAdapterMiseBackendResolveIdentity",
	} {
		if err := index.check(ref); err != nil {
			t.Errorf("check(%q) rejected real evidence: %v", ref, err)
		}
	}
}

func splitConsumerRefs(consumer string) []string {
	replacer := strings.NewReplacer(" + ", "\n", " and ", "\n")
	var refs []string
	for _, ref := range strings.Split(replacer.Replace(consumer), "\n") {
		if ref = strings.TrimSpace(ref); ref != "" {
			refs = append(refs, ref)
		}
	}
	return refs
}

// evidenceIndex records the Go declarations a consumer may name. Test
// functions are keyed by slash-separated directory plus name; production
// symbols are keyed by type/package and member.
type evidenceIndex struct {
	root         string
	testFuncs    map[string]bool
	types        map[string]bool
	methods      map[string]map[string]bool
	packageFuncs map[string]map[string]bool
}

func newEvidenceIndex(t *testing.T, root string) *evidenceIndex {
	t.Helper()
	index := &evidenceIndex{
		root:         root,
		testFuncs:    make(map[string]bool),
		types:        make(map[string]bool),
		methods:      make(map[string]map[string]bool),
		packageFuncs: make(map[string]map[string]bool),
	}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path == root {
				return nil
			}
			name := entry.Name()
			if strings.HasPrefix(name, ".") || name == "testdata" || name == "dist" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		dir := filepath.ToSlash(filepath.Dir(rel))
		isTest := strings.HasSuffix(path, "_test.go")
		for _, decl := range file.Decls {
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				name := decl.Name.Name
				if decl.Recv == nil {
					if isTest {
						if strings.HasPrefix(name, "Test") {
							index.testFuncs[dir+"/"+name] = true
						}
						continue
					}
					pkg := file.Name.Name
					if index.packageFuncs[pkg] == nil {
						index.packageFuncs[pkg] = make(map[string]bool)
					}
					index.packageFuncs[pkg][name] = true
					continue
				}
				if isTest {
					continue
				}
				receiver := receiverTypeName(decl.Recv)
				if receiver == "" {
					continue
				}
				if index.methods[receiver] == nil {
					index.methods[receiver] = make(map[string]bool)
				}
				index.methods[receiver][name] = true
			case *ast.GenDecl:
				if isTest || decl.Tok != token.TYPE {
					continue
				}
				for _, spec := range decl.Specs {
					typeSpec, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					index.types[typeSpec.Name.Name] = true
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("index Go sources under %s: %v", root, err)
	}
	return index
}

// check reports whether ref names existing evidence.
func (index *evidenceIndex) check(ref string) error {
	switch {
	case strings.HasSuffix(ref, ".go"):
		if !strings.HasSuffix(ref, "_test.go") {
			return fmt.Errorf("cited Go file is not a test file")
		}
		info, err := os.Stat(filepath.Join(index.root, filepath.FromSlash(ref)))
		if err != nil {
			return fmt.Errorf("cited test file does not exist")
		}
		if info.IsDir() {
			return fmt.Errorf("cited test path is a directory")
		}
		return nil
	case strings.Contains(ref, "/"):
		slash := strings.LastIndex(ref, "/")
		dir, name := ref[:slash], ref[slash+1:]
		if !strings.HasPrefix(name, "Test") {
			return fmt.Errorf("unsupported consumer reference format")
		}
		if !index.testFuncs[dir+"/"+name] {
			return fmt.Errorf("no %s test function in %s", name, dir)
		}
		return nil
	case strings.HasPrefix(ref, "Test"):
		if !index.testFuncs["internal/contracttest/"+ref] {
			return fmt.Errorf("no %s test function in internal/contracttest", ref)
		}
		return nil
	case strings.Contains(ref, "."):
		dot := strings.Index(ref, ".")
		qualifier, name := ref[:dot], ref[dot+1:]
		if index.methods[qualifier][name] || index.packageFuncs[qualifier][name] {
			return nil
		}
		if !index.types[qualifier] {
			if _, isPackage := index.packageFuncs[qualifier]; !isPackage {
				return fmt.Errorf("no type or package %q in the repository", qualifier)
			}
		}
		return fmt.Errorf("no method %s.%s or package function %s.%s", qualifier, name, qualifier, name)
	default:
		return fmt.Errorf("unsupported consumer reference format")
	}
}

func receiverTypeName(recv *ast.FieldList) string {
	if recv == nil || len(recv.List) == 0 {
		return ""
	}
	switch expr := recv.List[0].Type.(type) {
	case *ast.StarExpr:
		if ident, ok := expr.X.(*ast.Ident); ok {
			return ident.Name
		}
	case *ast.Ident:
		return expr.Name
	}
	return ""
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}
