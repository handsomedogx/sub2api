param(
    [ValidateSet('Start', 'Stop', 'Status', 'Logs', 'Rebuild', 'Build', 'Push')]
    [string]$Action = 'Start',
    [string]$Proxy = 'http://127.0.0.1:10808',
    [string]$ImageTag = ('local-' + (Get-Date -Format 'yyyyMMdd-HHmmss'))
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

function Invoke-Checked {
    param([string]$Program, [string[]]$Arguments)
    & $Program @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$Program failed with exit code $LASTEXITCODE" }
}

function New-RandomHex {
    $bytes = New-Object byte[] 32
    $rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    try { $rng.GetBytes($bytes) } finally { $rng.Dispose() }
    return ([BitConverter]::ToString($bytes)).Replace('-', '').ToLowerInvariant()
}

$envPath = Join-Path $PSScriptRoot '.env'
if (!(Test-Path -LiteralPath $envPath)) {
    if ($Action -notin @('Start', 'Rebuild', 'Build', 'Push')) { throw 'Run Start first to initialize the local environment.' }
    $settings = @(
        'SERVER_PORT=8080'
        'TZ=Asia/Shanghai'
        'RUN_MODE=standard'
        'ADMIN_EMAIL=admin@sub2api.local'
        "ADMIN_PASSWORD=$(New-RandomHex)"
        "POSTGRES_PASSWORD=$(New-RandomHex)"
        "REDIS_PASSWORD=$(New-RandomHex)"
        "JWT_SECRET=$(New-RandomHex)"
        "TOTP_ENCRYPTION_KEY=$(New-RandomHex)"
    )
    [IO.File]::WriteAllLines($envPath, $settings, (New-Object Text.UTF8Encoding $false))
    Write-Host 'Generated deploy/dog/.env. Keep this private and back it up with the data volumes.'
}

$oldCommit = $env:SUB2API_COMMIT
$oldVersion = $env:SUB2API_VERSION
$oldHttpProxy = $env:HTTP_PROXY
$oldHttpsProxy = $env:HTTPS_PROXY
if ($Action -in @('Start', 'Rebuild', 'Build', 'Push')) {
    $commit = & git -C (Join-Path $PSScriptRoot '../..') rev-parse --short=12 HEAD
    if ($LASTEXITCODE -ne 0) { throw 'Could not resolve source commit.' }
    $env:SUB2API_COMMIT = $commit
    if (!$env:SUB2API_VERSION) {
        $baseVersion = (Get-Content (Join-Path $PSScriptRoot '../../backend/cmd/server/VERSION') -Raw).Trim()
        $env:SUB2API_VERSION = "$baseVersion-dog"
    }
    if ($Proxy) {
        $env:HTTP_PROXY = $Proxy
        $env:HTTPS_PROXY = $Proxy
    }
}
Push-Location $PSScriptRoot
try {
    $composeArgs = @('compose', '--project-directory', $PSScriptRoot, '--env-file', $envPath, '-f', (Join-Path $PSScriptRoot 'compose.yml'))
    Invoke-Checked docker ($composeArgs + @('config', '--quiet'))
    switch ($Action) {
        'Build'   { Invoke-Checked docker ($composeArgs + @('build', 'sub2api')) }
        'Push'    {
            if ($ImageTag -notmatch '^local-[a-zA-Z0-9_][a-zA-Z0-9_.-]{0,121}$') {
                throw 'Local pushes require a Docker-compatible tag starting with local-.'
            }
            Invoke-Checked docker ($composeArgs + @('build', 'sub2api'))
            $images = & docker @composeArgs config --images
            if ($LASTEXITCODE -ne 0) { throw 'Could not resolve the built image.' }
            $localImage = @($images | Where-Object { $_ -like 'sub2api-dog:*' })
            if ($localImage.Count -ne 1) { throw 'Expected one local sub2api-dog image.' }
            $targetImage = "ghcr.io/handsomedogx/sub2api-dog:$ImageTag"
            Invoke-Checked docker (@('tag', $localImage[0], $targetImage))
            Write-Host 'Pushing via the local network. Log in with docker login ghcr.io -u handsomedogx if required.'
            Invoke-Checked docker (@('push', $targetImage))
            Write-Host "Published $targetImage"
        }
        'Start'   { Invoke-Checked docker ($composeArgs + @('up', '-d', '--build', '--wait', '--wait-timeout', '300')) }
        'Rebuild' { Invoke-Checked docker ($composeArgs + @('up', '-d', '--build', '--wait', '--wait-timeout', '300')) }
        'Stop'    { Invoke-Checked docker ($composeArgs + @('stop')) }
        'Status'  { Invoke-Checked docker ($composeArgs + @('ps')) }
        'Logs'    { Invoke-Checked docker ($composeArgs + @('logs', '--tail', '100', '-f', 'sub2api')) }
    }
} finally {
    Pop-Location
    $env:SUB2API_COMMIT = $oldCommit
    $env:SUB2API_VERSION = $oldVersion
    $env:HTTP_PROXY = $oldHttpProxy
    $env:HTTPS_PROXY = $oldHttpsProxy
}
