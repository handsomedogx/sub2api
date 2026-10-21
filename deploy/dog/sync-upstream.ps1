$ErrorActionPreference = 'Stop'
$repo = (Resolve-Path (Join-Path $PSScriptRoot '../..')).Path
Push-Location $repo
try {
    $branch = & git branch --show-current
    if ($LASTEXITCODE -ne 0 -or $branch -ne 'dog/develop') { throw 'Switch to dog/develop before synchronizing.' }
    $changes = & git status --porcelain
    if ($LASTEXITCODE -ne 0) { throw 'Could not inspect working tree.' }
    if ($changes) { throw 'Commit or stash your changes before synchronizing upstream.' }
    & git fetch upstream production
    if ($LASTEXITCODE -ne 0) { throw 'Fetch failed. Check the local GitHub proxy setting.' }
    & git merge --no-edit upstream/production
    if ($LASTEXITCODE -ne 0) { throw 'Merge needs attention. Resolve conflicts, or use git merge --abort to cancel.' }
    Write-Host 'Upstream merged locally. Review and test, then push to your origin when ready.'
} finally { Pop-Location }
