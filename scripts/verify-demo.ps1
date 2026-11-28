param([switch]$ProcessRestart)
$ErrorActionPreference = 'Stop'
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$nodeCommand = Get-Command node -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
if (-not $nodeCommand) {
    throw 'Node.js 22+ is required for web checks. Add node to PATH and reopen the terminal. For Go-only checks, use the three Go commands in README.md.'
}
$nodeVersion = & $nodeCommand.Source -p 'process.versions.node'
if ($LASTEXITCODE -ne 0) { throw 'Could not read the Node.js version.' }
if ([version]$nodeVersion -lt [version]'22.0.0') {
    throw ('Node.js 22+ is required for web checks; found ' + $nodeVersion + '. See README.md for Go-only checks.')
}
if (-not (Get-Command go -CommandType Application -ErrorAction SilentlyContinue)) {
    throw 'Go is required for application checks. Add go to PATH and reopen the terminal; see README.md for the required toolchain.'
}
function Invoke-GoCheck([string[]]$GoArguments) {
    & go @GoArguments
    if ($LASTEXITCODE -ne 0) { throw ('Go check failed: ' + ($GoArguments -join ' ')) }
}
Push-Location $repoRoot
try {
    & node --check (Join-Path $repoRoot 'internal/webapp/static/manga.js')
    if ($LASTEXITCODE -ne 0) { throw 'Web JavaScript syntax check failed' }
    & node --test (Join-Path $repoRoot 'scripts/test-web.cjs')
    if ($LASTEXITCODE -ne 0) { throw 'Web behavior tests failed' }
    Invoke-GoCheck @('test','-count=1','./...')
    Invoke-GoCheck @('vet','./...')
    Invoke-GoCheck @('build','./cmd/backend','./cmd/catalog','./cmd/tui')
    if ($ProcessRestart) {
        $previousProcessSwitch = $env:MANGAHUB_PROCESS_TEST
        try {
            $env:MANGAHUB_PROCESS_TEST = '1'
            Invoke-GoCheck @('test','-count=1','-run','TestMangaProcessRestart','-timeout','120s','-v','./internal/tui')
        } finally {
            if ($null -eq $previousProcessSwitch) { Remove-Item Env:MANGAHUB_PROCESS_TEST -ErrorAction SilentlyContinue }
            else { $env:MANGAHUB_PROCESS_TEST = $previousProcessSwitch }
        }
    }
} finally { Pop-Location }
