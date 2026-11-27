param([switch]$ProcessRestart)
$ErrorActionPreference = 'Stop'
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
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
