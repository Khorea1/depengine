package bundle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Khorea1/depengine/internal/plan"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const formatVersion = 1

// Entry binds a locked artifact URL to the content-addressed payload carrying
// its locked SHA-256 identity. The lock remains the authority for all metadata.
type Entry struct {
	Tool     string `json:"tool"`
	Method   string `json:"method"`
	URL      string `json:"url"`
	Checksum string `json:"checksum"`
	Payload  string `json:"payload"`
}

type manifest struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

// Open validates the entire deterministic bundle before returning any payload
// paths. Symlinks, traversal, duplicates, unknown entries and corrupt bytes
// are rejected; callers must still compare the returned entries with the lock.
func Open(root string) ([]Entry, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("bundle: resolve root: %w", err)
	}
	for current := absolute; ; current = filepath.Dir(current) {
		info, inspectErr := os.Lstat(current)
		if inspectErr != nil {
			return nil, fmt.Errorf("bundle: inspect path component: %w", inspectErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("bundle: symlink path component %s", current)
		}
		if current == filepath.Dir(current) {
			break
		}
	}
	root = absolute
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return nil, fmt.Errorf("bundle: inspect root: %w", err)
	}
	if !rootInfo.IsDir() {
		return nil, errors.New("bundle: root is not a directory")
	}
	manifestPath := filepath.Join(root, "manifest.json")
	manifestInfo, err := os.Lstat(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("bundle: inspect manifest: %w", err)
	}
	if !manifestInfo.Mode().IsRegular() {
		return nil, errors.New("bundle: manifest is not a regular file")
	}
	f, err := os.Open(manifestPath) // #nosec G304 -- fixed manifest name under the checked, symlink-free bundle root.
	if err != nil {
		return nil, fmt.Errorf("bundle: open manifest: %w", err)
	}
	defer func() { _ = f.Close() }()
	dec := json.NewDecoder(io.LimitReader(f, 16<<20))
	dec.DisallowUnknownFields()
	var m manifest
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("bundle: decode manifest: %w", err)
	}
	if m.Version != formatVersion {
		return nil, fmt.Errorf("bundle: unsupported format version %d", m.Version)
	}
	payloadDir := filepath.Join(root, "payloads")
	payloadInfo, err := os.Lstat(payloadDir)
	if err != nil {
		return nil, fmt.Errorf("bundle: inspect payload directory: %w", err)
	}
	if !payloadInfo.IsDir() {
		return nil, errors.New("bundle: payload directory is not a directory")
	}
	files, err := os.ReadDir(payloadDir)
	if err != nil {
		return nil, fmt.Errorf("bundle: read payload directory: %w", err)
	}
	expectedPayloads := make(map[string]struct{}, len(m.Entries))
	for _, entry := range m.Entries {
		expectedPayloads[filepath.Base(entry.Payload)] = struct{}{}
	}
	if len(files) != len(expectedPayloads) {
		return nil, errors.New("bundle: payload directory has missing or unreferenced files")
	}
	var trailing any
	if err := json.NewDecoder(f).Decode(&trailing); err != io.EOF {
		return nil, errors.New("bundle: trailing manifest data")
	}
	seen := make(map[string]bool, len(m.Entries))
	for _, e := range m.Entries {
		if e.Tool == "" || e.Method == "" || e.URL == "" || !validSHA256(e.Checksum) {
			return nil, errors.New("bundle: invalid artifact identity")
		}
		if e.Payload != "payloads/"+strings.TrimPrefix(e.Checksum, "sha256:") {
			return nil, fmt.Errorf("bundle: non-canonical payload path %q", e.Payload)
		}
		if seen[e.Tool+"\x00"+e.Method+"\x00"+e.URL] {
			return nil, errors.New("bundle: duplicate artifact identity")
		}
		seen[e.Tool+"\x00"+e.Method+"\x00"+e.URL] = true
		p := filepath.Join(root, filepath.FromSlash(e.Payload))
		st, err := os.Lstat(p)
		if err != nil {
			return nil, fmt.Errorf("bundle: payload %s: %w", e.Payload, err)
		}
		if !st.Mode().IsRegular() {
			return nil, fmt.Errorf("bundle: payload %s is not a regular file", e.Payload)
		}
		if err := verify(p, e.Checksum); err != nil {
			return nil, fmt.Errorf("bundle: %s: %w", e.Payload, err)
		}
	}
	for _, file := range files {
		if file.Type()&os.ModeSymlink != 0 || file.IsDir() {
			return nil, fmt.Errorf("bundle: invalid payload entry %s", file.Name())
		}
		if _, found := expectedPayloads[file.Name()]; !found {
			return nil, fmt.Errorf("bundle: unreferenced payload %s", file.Name())
		}
	}
	return m.Entries, nil
}

func validSHA256(s string) bool {
	if !strings.HasPrefix(s, "sha256:") || len(s) != len("sha256:")+64 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(s, "sha256:"))
	return err == nil
}

func verify(path, checksum string) error {
	f, err := os.Open(path) // #nosec G304 -- caller supplies a validated bundle payload or a new content-addressed export path.
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if "sha256:"+hex.EncodeToString(h.Sum(nil)) != strings.ToLower(checksum) {
		return errors.New("payload checksum mismatch")
	}
	return nil
}

// ArtifactEntries returns downloadable lock identities supported by this
// format. Signature-backed identities are deliberately rejected until their
// sidecar/key material can be carried and verified offline.
func ArtifactEntries(doc plan.LockDocument) ([]Entry, error) {
	var entries []Entry
	for _, p := range doc.Entries {
		for _, a := range p.Identity.Artifacts {
			if a.URL == "" {
				continue
			}
			if a.SignatureURL != "" || a.SignaturePath != "" || a.SigningKey != "" {
				return nil, fmt.Errorf("bundle: signature-backed artifact %s/%s is unsupported", p.Tool.Name, p.Candidate.Method)
			}
			if !validSHA256(a.Checksum) {
				return nil, fmt.Errorf("bundle: artifact %s/%s requires a fixed SHA-256 checksum", p.Tool.Name, p.Candidate.Method)
			}
			entries = append(entries, Entry{Tool: p.Tool.Name, Method: p.Candidate.Method, URL: a.URL, Checksum: a.Checksum, Payload: "payloads/" + strings.TrimPrefix(strings.ToLower(a.Checksum), "sha256:")})
		}
	}
	return entries, nil
}

// Export downloads every supported locked payload and writes a canonical
// directory bundle. Destination must not already exist.
func Export(root string, doc plan.LockDocument) error {
	return ExportContext(context.Background(), root, doc)
}

func ExportContext(ctx context.Context, root string, doc plan.LockDocument) error {
	entries, err := ArtifactEntries(doc)
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Tool != entries[j].Tool {
			return entries[i].Tool < entries[j].Tool
		}
		if entries[i].Method != entries[j].Method {
			return entries[i].Method < entries[j].Method
		}
		return entries[i].URL < entries[j].URL
	})
	if err := os.Mkdir(root, 0o700); err != nil {
		return fmt.Errorf("bundle: create destination: %w", err)
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(root)
		}
	}()
	payloadDir := filepath.Join(root, "payloads")
	if err := os.Mkdir(payloadDir, 0o700); err != nil {
		return err
	}
	client := &http.Client{}
	maxBytes := int64(4 << 30)
	if raw := os.Getenv("DEPENGINE_DOWNLOAD_MAX_BYTES"); raw != "" {
		if n, parseErr := strconv.ParseInt(raw, 10, 64); parseErr == nil && n > 0 {
			maxBytes = n
		}
	}
	writtenPayloads := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		if _, ok := writtenPayloads[e.Payload]; ok {
			continue
		}
		path := filepath.Join(root, filepath.FromSlash(e.Payload))
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.URL, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("bundle: download %s/%s: %w", e.Tool, e.Method, err)
		}
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			return fmt.Errorf("bundle: download %s/%s: HTTP %s", e.Tool, e.Method, resp.Status)
		}
		if resp.ContentLength > maxBytes {
			_ = resp.Body.Close()
			return fmt.Errorf("bundle: download exceeds %d-byte limit", maxBytes)
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304 -- path is a checksum-derived child of the newly created bundle directory.
		if err != nil {
			_ = resp.Body.Close()
			return err
		}
		n, copyErr := io.Copy(f, io.LimitReader(resp.Body, maxBytes+1))
		closeBodyErr := resp.Body.Close()
		closeFileErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeBodyErr != nil {
			return closeBodyErr
		}
		if closeFileErr != nil {
			return closeFileErr
		}
		if n > maxBytes {
			return fmt.Errorf("bundle: download exceeds %d-byte limit", maxBytes)
		}
		if err := verify(path, e.Checksum); err != nil {
			return fmt.Errorf("bundle: downloaded %s/%s: %w", e.Tool, e.Method, err)
		}
		writtenPayloads[e.Payload] = struct{}{}
	}
	data, err := json.MarshalIndent(manifest{Version: formatVersion, Entries: entries}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), data, 0o600); err != nil {
		return err
	}
	ok = true
	return nil
}
