param([ValidatePattern('^(XX|[0-9]{1,3})$')][string]$Group = 'XX')
$ErrorActionPreference = 'Stop'
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$distRoot = Join-Path $repoRoot 'dist'
New-Item -ItemType Directory -Path $distRoot -Force | Out-Null
$zipPath = Join-Path $distRoot ('Group' + $Group + '_MangaHub_CodeOnly.zip')
$tracked = & git -C $repoRoot -c core.quotepath=false ls-files --cached
if ($LASTEXITCODE -ne 0) { throw 'Could not enumerate tracked source files' }
$paths = @($tracked | Where-Object {
    ($_ -match '^(cmd|gen|internal|proto|docs|scripts)/' -or $_ -in @('README.md','go.mod','go.sum','.env.example','.gitignore','buf.yaml','buf.gen.yaml','LICENSE')) -and
    $_ -notmatch '(^|/)(REPORT[^/]*\.(md|pdf)|\.env)$' -and
    $_ -notmatch '\.(db|sqlite|sqlite3|exe|zip|log)$'
})
if ($paths.Count -eq 0) { throw 'No source files selected' }
Add-Type -AssemblyName System.IO.Compression
$fileStream = [IO.File]::Open($zipPath, [IO.FileMode]::Create, [IO.FileAccess]::Write)
$archive = [IO.Compression.ZipArchive]::new($fileStream, [IO.Compression.ZipArchiveMode]::Create)
try {
    foreach ($relative in $paths) {
        $source = [IO.Path]::GetFullPath((Join-Path $repoRoot $relative))
        if (-not $source.StartsWith($repoRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) { throw 'Source outside repository' }
        $item = Get-Item -LiteralPath $source -Force
        if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Redirected source is not allowed' }
        $entry = $archive.CreateEntry('MangaHub/' + $relative.Replace('\','/'), [IO.Compression.CompressionLevel]::Optimal)
        $inputStream = [IO.File]::OpenRead($source)
        $outputStream = $entry.Open()
        try { $inputStream.CopyTo($outputStream) } finally { $inputStream.Dispose(); $outputStream.Dispose() }
    }
    $entry = $archive.CreateEntry('MangaHub/PACKAGE_README.txt')
    $writer = [IO.StreamWriter]::new($entry.Open())
    try {
        $writer.WriteLine('MangaHub code-only review package. REPORT.pdf and report drafts are intentionally excluded.')
        $writer.WriteLine('Do not submit as the final assignment archive until REPORT.pdf is added and the group number is confirmed.')
        $writer.WriteLine('No personal databases, local secrets, Git metadata, or build caches are included.')
        $writer.WriteLine('Start from README.md. The required demonstration uses the Go tview TUI.')
    } finally { $writer.Dispose() }
} finally { $archive.Dispose(); $fileStream.Dispose() }
$hash = Get-FileHash -LiteralPath $zipPath -Algorithm SHA256
[pscustomobject]@{ Archive = $zipPath; SourceFiles = $paths.Count; SHA256 = $hash.Hash }
