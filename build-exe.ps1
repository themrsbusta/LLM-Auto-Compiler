$ErrorActionPreference = 'Stop'
Push-Location $PSScriptRoot
try {
    & python -m PyInstaller --noconfirm --clean --onefile --console --name LLMAuc --hidden-import pathlib --hidden-import runpy --workpath .packaging-build --distpath ../build autocompiler.py
    if ($LASTEXITCODE -ne 0) { throw 'Falha no empacotamento' }
    foreach ($name in @('config.example.json','README.md','LICENSE')) {
        Copy-Item -LiteralPath $name -Destination (Join-Path '../build' $name)
    }
    $packageFiles = @('../build/LLMAuc.exe','../build/config.example.json','../build/README.md','../build/LICENSE')
    Compress-Archive -LiteralPath $packageFiles -DestinationPath '../LLMAuc-0.1-windows-x64-public.zip' -Force
} finally { Pop-Location }
