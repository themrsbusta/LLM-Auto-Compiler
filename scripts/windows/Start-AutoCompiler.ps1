param([Parameter(Mandatory=$true)][string]$Objective, [string]$RunDir)
$root = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
if (-not $RunDir) { $RunDir = Join-Path $root ("runs/" + (Get-Date -Format "yyyyMMdd-HHmmss-fff")) }
& python (Join-Path $root 'src/autocompiler.py') --config (Join-Path $root 'config.json') --objective $Objective --run-dir $RunDir
exit $LASTEXITCODE
