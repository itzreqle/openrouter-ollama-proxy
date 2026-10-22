# orollama: plain `claude` uses the local proxy when it is running (Windows PowerShell / pwsh).
# Install once from the repo:
#   .\scripts\install-cli.ps1
# Or add to your PowerShell profile:
#   . "C:\path\to\openrouter-ollama-proxy\scripts\shell\orollama-claude.ps1"

$ErrorActionPreference = "Stop"

function Get-OrollamaRoot {
    $here = $PSScriptRoot
    if (-not $here) {
        $here = Split-Path -Parent $MyInvocation.MyCommand.Path
    }
    return (Resolve-Path (Join-Path $here "..\..")).Path
}

function Get-OrollamaEnvValue {
    param(
        [string]$File,
        [string]$Key
    )
    if (-not (Test-Path $File)) { return "" }
    $line = Get-Content $File | Where-Object { $_ -match "^$Key=" } | Select-Object -Last 1
    if (-not $line) { return "" }
    $val = $line.Substring($Key.Length + 1).Trim()
    return $val.Trim('"').Trim("'")
}

function Test-OrollamaProxy {
    param([string]$Url = "http://127.0.0.1:8082")
    try {
        $resp = Invoke-WebRequest -Uri "$Url/" -UseBasicParsing -TimeoutSec 1
        return $resp.StatusCode -ge 200 -and $resp.StatusCode -lt 500
    } catch {
        return $false
    }
}

function Write-OrollamaStatus {
    param([string]$Url)
    Write-Host "orollama: " -NoNewline -ForegroundColor Cyan
    Write-Host "proxy ON  " -NoNewline -ForegroundColor Green
    Write-Host $Url -ForegroundColor Gray
    Write-Host "  provider  " -NoNewline -ForegroundColor DarkCyan
    Write-Host "OpenRouter + local Ollama"
    Write-Host "  opus      " -NoNewline -ForegroundColor Magenta
    Write-Host $env:ANTHROPIC_DEFAULT_OPUS_MODEL
    Write-Host "  sonnet    " -NoNewline -ForegroundColor Magenta
    Write-Host $env:ANTHROPIC_DEFAULT_SONNET_MODEL
    Write-Host "  haiku     " -NoNewline -ForegroundColor Magenta
    Write-Host $env:ANTHROPIC_DEFAULT_HAIKU_MODEL
}

$script:OrollamaRoot = Get-OrollamaRoot
$script:OrollamaEnvFile = if ($env:OROLLAMA_ENV) { $env:OROLLAMA_ENV } else { Join-Path $script:OrollamaRoot ".env" }
$script:OrollamaProxyUrl = if ($env:OROLLAMA_URL) { $env:OROLLAMA_URL } else { "http://127.0.0.1:8082" }

function claude {
    $claudeCmd = Get-Command claude.cmd, claude.exe, claude -ErrorAction SilentlyContinue |
        Where-Object { $_.Source -notmatch "orollama" } |
        Select-Object -First 1

    if (-not $claudeCmd) {
        Write-Host "orollama: 'claude' not found on PATH. Install Claude Code first." -ForegroundColor Red
        return
    }

    if (Test-OrollamaProxy $script:OrollamaProxyUrl) {
        if (-not (Test-Path $script:OrollamaEnvFile)) {
            Write-Host "orollama: proxy is up but missing $($script:OrollamaEnvFile)" -ForegroundColor Red
            return
        }
        $token = Get-OrollamaEnvValue $script:OrollamaEnvFile "PROXY_AUTH_TOKEN"
        $opus = Get-OrollamaEnvValue $script:OrollamaEnvFile "MODEL_OPUS"
        $sonnet = Get-OrollamaEnvValue $script:OrollamaEnvFile "MODEL_SONNET"
        $haiku = Get-OrollamaEnvValue $script:OrollamaEnvFile "MODEL_HAIKU"

        $env:ANTHROPIC_BASE_URL = $script:OrollamaProxyUrl
        $env:ANTHROPIC_API_KEY = ""
        $env:ANTHROPIC_AUTH_TOKEN = if ($token) { $token } else { "orollama" }
        if ($opus) { $env:ANTHROPIC_DEFAULT_OPUS_MODEL = $opus }
        if ($sonnet) {
            $env:ANTHROPIC_DEFAULT_SONNET_MODEL = $sonnet
            $env:ANTHROPIC_MODEL = $sonnet
        }
        if ($haiku) { $env:ANTHROPIC_DEFAULT_HAIKU_MODEL = $haiku }
        Write-OrollamaStatus $script:OrollamaProxyUrl
    } else {
        Remove-Item Env:ANTHROPIC_BASE_URL -ErrorAction SilentlyContinue
        Remove-Item Env:ANTHROPIC_AUTH_TOKEN -ErrorAction SilentlyContinue
        Remove-Item Env:ANTHROPIC_MODEL -ErrorAction SilentlyContinue
        Remove-Item Env:ANTHROPIC_DEFAULT_OPUS_MODEL -ErrorAction SilentlyContinue
        Remove-Item Env:ANTHROPIC_DEFAULT_SONNET_MODEL -ErrorAction SilentlyContinue
        Remove-Item Env:ANTHROPIC_DEFAULT_HAIKU_MODEL -ErrorAction SilentlyContinue
        Write-Host "orollama: " -NoNewline -ForegroundColor Cyan
        Write-Host "proxy OFF" -NoNewline -ForegroundColor Yellow
        Write-Host " — launching Claude Code without local gateway"
    }

    & $claudeCmd.Source @args
}
