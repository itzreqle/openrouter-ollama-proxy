# Windows installer for orollama Claude Code integration (PowerShell).
# Usage (from repo root):
#   Set-ExecutionPolicy -Scope CurrentUser RemoteSigned   # once, if needed
#   .\scripts\install-cli.ps1

$ErrorActionPreference = "Stop"
$Root = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$Hook = Join-Path $Root "scripts\shell\orollama-claude.ps1"
$Mark = "orollama-claude shell hook"

if (-not (Test-Path $Hook)) {
    Write-Error "Missing $Hook"
}

$profilePath = $PROFILE
$profileDir = Split-Path -Parent $profilePath
if (-not (Test-Path $profileDir)) {
    New-Item -ItemType Directory -Path $profileDir -Force | Out-Null
}
if (-not (Test-Path $profilePath)) {
    New-Item -ItemType File -Path $profilePath -Force | Out-Null
}

$line = ". `"$Hook`""
$content = Get-Content $profilePath -Raw -ErrorAction SilentlyContinue
if ($content -and $content.Contains($Mark)) {
    Write-Host "PowerShell hook already present in $profilePath" -ForegroundColor Yellow
} else {
    Add-Content -Path $profilePath -Value "`n# $Mark`n$line`n"
    Write-Host "Added PowerShell hook to $profilePath" -ForegroundColor Green
}

Write-Host ""
Write-Host "Reload profile:  . `$PROFILE" -ForegroundColor Cyan
Write-Host "Terminal 1:      go run ./cmd/orollama   (or make run in Git Bash)" -ForegroundColor Cyan
Write-Host "Terminal 2:      claude" -ForegroundColor Cyan
