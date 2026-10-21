param(
    [string]$Tag = ('dog-' + (Get-Date -Format 'yyyyMMdd-HHmmss'))
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
if ($Tag -notmatch '^dog-[a-zA-Z0-9_][a-zA-Z0-9_.-]{0,123}$') {
    throw 'Use a Docker-compatible release tag starting with dog-.'
}
$repo = (Resolve-Path (Join-Path $PSScriptRoot '../..')).Path
Push-Location $repo
try {
    $branch = & git branch --show-current
    if ($LASTEXITCODE -ne 0 -or $branch -ne 'dog/develop') { throw 'Switch to dog/develop before publishing.' }
    $changes = & git status --porcelain
    if ($LASTEXITCODE -ne 0) { throw 'Could not inspect working tree.' }
    if ($changes) { throw 'Commit your changes first. Cloud builds use committed source only.' }
    $origin = & git remote get-url origin
    if ($LASTEXITCODE -ne 0 -or $origin -notmatch 'github\.com[:/]handsomedogx/sub2api(?:\.git)?$') {
        throw 'origin must point to your handsomedogx/sub2api fork.'
    }
    & git rev-parse --verify --quiet "refs/tags/$Tag" *> $null
    if ($LASTEXITCODE -eq 0) { throw 'This tag already exists. Choose a new version tag.' }
    & git tag -a $Tag -m "Publish personal container image $Tag"
    if ($LASTEXITCODE -ne 0) { throw 'Could not create release tag.' }
    & git push --atomic origin 'refs/heads/dog/develop:refs/heads/dog/develop' "refs/tags/${Tag}:refs/tags/${Tag}"
    if ($LASTEXITCODE -ne 0) {
        throw "Push failed. The local tag $Tag remains; retry the git push command after fixing authentication/network access."
    }
    Write-Host 'Cloud build requested. Check https://github.com/handsomedogx/sub2api/actions'
    Write-Host "After success: ghcr.io/handsomedogx/sub2api:$Tag"
} finally { Pop-Location }
