# orollama: plain `claude` uses the local proxy when it is running (Windows PowerShell / pwsh).
# Install once from the repo:
#   .\scripts\install-cli.ps1

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

function Import-OrollamaResolvedEnv {
    $path = Join-Path $HOME ".cache\orollama\resolved.env"
    if (-not (Test-Path $path)) { return }
    Get-Content $path | ForEach-Object {
        if ($_ -match '^\s*#' -or $_ -notmatch '=') { return }
        $k, $v = $_.Split('=', 2)
        Set-Item -Path "Env:$k" -Value $v
    }
}

function ConvertTo-OrollamaGatewayId {
    param([string]$Id)
    if (-not $Id) { return $Id }
    if ($Id -match '\[1[mM]\]$') { $Id = $Id -replace '\[1[mM]\]$', '' }
    if ($Id.StartsWith("openrouter-claude/") -or $Id.StartsWith("ollama-claude/")) { return $Id }
    if ($Id.StartsWith("openrouter/")) { return "openrouter-claude/" + $Id.Substring("openrouter/".Length) }
    if ($Id.StartsWith("ollama/")) { return "ollama-claude/" + $Id.Substring("ollama/".Length) }
    return $Id
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
    if ($env:CLAUDE_CODE_MAX_CONTEXT_TOKENS) {
        Write-Host "  context   " -NoNewline -ForegroundColor DarkCyan
        Write-Host $env:CLAUDE_CODE_MAX_CONTEXT_TOKENS
    }
    Write-Host "  if auth warning: run  claude /logout  then relaunch" -ForegroundColor Yellow
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
        # Drop stale pins from older openrouter/ ids.
        Remove-Item Env:MODEL_OPUS, Env:MODEL_SONNET, Env:MODEL_HAIKU, Env:MODEL -ErrorAction SilentlyContinue
        Remove-Item Env:ANTHROPIC_DEFAULT_OPUS_MODEL, Env:ANTHROPIC_DEFAULT_SONNET_MODEL, Env:ANTHROPIC_DEFAULT_HAIKU_MODEL, Env:ANTHROPIC_MODEL -ErrorAction SilentlyContinue

        Import-OrollamaResolvedEnv

        $token = Get-OrollamaEnvValue $script:OrollamaEnvFile "PROXY_AUTH_TOKEN"
        $opus = Get-OrollamaEnvValue $script:OrollamaEnvFile "MODEL_OPUS"
        $sonnet = Get-OrollamaEnvValue $script:OrollamaEnvFile "MODEL_SONNET"
        $haiku = Get-OrollamaEnvValue $script:OrollamaEnvFile "MODEL_HAIKU"
        if (-not $opus) { $opus = $env:OROLLAMA_MODEL_OPUS }
        if (-not $sonnet) { $sonnet = $env:OROLLAMA_MODEL_SONNET }
        if (-not $haiku) { $haiku = $env:OROLLAMA_MODEL_HAIKU }
        $opus = ConvertTo-OrollamaGatewayId $opus
        $sonnet = ConvertTo-OrollamaGatewayId $sonnet
        $haiku = ConvertTo-OrollamaGatewayId $haiku

        $env:ANTHROPIC_BASE_URL = $script:OrollamaProxyUrl
        $env:ANTHROPIC_API_KEY = ""
        $env:ANTHROPIC_AUTH_TOKEN = if ($token) { $token } else { "orollama" }
        $env:CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY = "1"
        if (-not $env:CLAUDE_CODE_GATEWAY_MODEL_DISCOVERY_TIMEOUT_MS) {
            $env:CLAUDE_CODE_GATEWAY_MODEL_DISCOVERY_TIMEOUT_MS = "15000"
        }
        if (-not $env:CLAUDE_CODE_MAX_CONTEXT_TOKENS) {
            $env:CLAUDE_CODE_MAX_CONTEXT_TOKENS = "1000000"
        }
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
        Remove-Item Env:CLAUDE_CODE_MAX_CONTEXT_TOKENS -ErrorAction SilentlyContinue
        Remove-Item Env:CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY -ErrorAction SilentlyContinue
        Remove-Item Env:CLAUDE_CODE_GATEWAY_MODEL_DISCOVERY_TIMEOUT_MS -ErrorAction SilentlyContinue
        Write-Host "orollama: " -NoNewline -ForegroundColor Cyan
        Write-Host "proxy OFF" -NoNewline -ForegroundColor Yellow
        Write-Host " — launching Claude Code without local gateway"
    }

    & $claudeCmd.Source @args
}
