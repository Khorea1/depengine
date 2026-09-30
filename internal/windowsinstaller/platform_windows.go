//go:build windows

package windowsinstaller

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/internal/run"
)

type windowsCatalog struct{}

func newCatalog() packageCatalog { return windowsCatalog{} }

func (windowsCatalog) Find(ctx context.Context, rn run.Runner, kind, name, publisher string) (string, bool, error) {
	if rn == nil {
		return "", false, fmt.Errorf("no runner")
	}
	// Identity values are base64-encoded before crossing PowerShell's command
	// parser. The script decodes them as data and never interpolates them.
	script := `param($kind64,$name64,$publisher64)
$decode = { param($value) [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($value)) }
$kind = & $decode $kind64
$name = & $decode $name64
$publisher = & $decode $publisher64
if ($kind -eq 'exe') {
  $roots = @('HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall','HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall','HKLM:\Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall')
  foreach ($root in $roots) {
    foreach ($key in Get-ChildItem -LiteralPath $root -ErrorAction SilentlyContinue) {
      $item = Get-ItemProperty -LiteralPath $key.PSPath -ErrorAction SilentlyContinue
      if ($item.DisplayName -ceq $name -and $item.Publisher -ceq $publisher) { Write-Output 'present'; exit 0 }
    }
  }
  exit 0
}
Get-AppxPackage -Name $name -ErrorAction Stop |
  Where-Object { $_.Name -ceq $name -and $_.Publisher -ceq $publisher } |
  Select-Object -First 1 -ExpandProperty PackageFullName`
	res := rn.Run(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script,
		encodeScriptArg(kind), encodeScriptArg(name), encodeScriptArg(publisher))
	if res.Err != nil {
		return "", false, fmt.Errorf("PowerShell package query failed: %w", res.Err)
	}
	if res.ExitCode != 0 {
		return "", false, fmt.Errorf("PowerShell package query exited %d: %s", res.ExitCode, strings.TrimSpace(string(res.Stderr)))
	}
	identity := strings.TrimSpace(string(res.Stdout))
	return identity, identity != "", nil
}

func installPackage(ctx context.Context, rn run.Runner, path, kind string) error {
	script := `param($path64)
$path = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($path64))
Add-AppxPackage -LiteralPath $path -ErrorAction Stop`
	res := rn.Run(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script, encodeScriptArg(path))
	if res.Err != nil {
		return fmt.Errorf("%s: signed package install failed: %w", kind, res.Err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("%s: signed package install exited %d: %s", kind, res.ExitCode, strings.TrimSpace(string(res.Stderr)))
	}
	return nil
}

func removePackage(ctx context.Context, rn run.Runner, fullName string) error {
	script := `param($identity64)
$identity = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($identity64))
Remove-AppxPackage -Package $identity -ErrorAction Stop`
	res := rn.Run(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script, encodeScriptArg(fullName))
	if res.Err != nil {
		return fmt.Errorf("AppX package removal failed: %w", res.Err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("AppX package removal exited %d: %s", res.ExitCode, strings.TrimSpace(string(res.Stderr)))
	}
	return nil
}

func encodeScriptArg(value string) string {
	return base64.StdEncoding.EncodeToString([]byte(value))
}
