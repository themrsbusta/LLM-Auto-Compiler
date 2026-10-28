$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
Push-Location -LiteralPath $root
try {
    $out = Join-Path $root 'build/windows-x64'
    $packages = Join-Path $root 'build/packages'
    & python -m PyInstaller --noconfirm --clean --workpath (Join-Path $root 'build/pyinstaller') --distpath $out (Join-Path $root 'packaging/LLMAuc.spec')
    if ($LASTEXITCODE -ne 0) { throw 'Falha no empacotamento' }
    foreach ($name in @('README.md','LICENSE','VERSION')) {
        Copy-Item -LiteralPath (Join-Path $root $name) -Destination $out -Force
    }
    Copy-Item -LiteralPath (Join-Path $root 'configs/config.example.json') -Destination $out -Force
    Copy-Item -LiteralPath (Join-Path $root 'docs/RELEASE-NOTES.md') -Destination $out -Force
    New-Item -ItemType Directory -Path $packages -Force | Out-Null
    $version = (Get-Content -LiteralPath (Join-Path $root 'VERSION') -Raw).Trim()
    $packageFiles = @('LLMAuc.exe','config.example.json','README.md','LICENSE','VERSION','RELEASE-NOTES.md') | ForEach-Object { Join-Path $out $_ }
    Compress-Archive -LiteralPath $packageFiles -DestinationPath (Join-Path $packages "LLMAuc-$version-windows-x64-public.zip") -Force
} finally { Pop-Location }
