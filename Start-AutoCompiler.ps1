param([Parameter(Mandatory=$true)][string]$Objective, [string]$RunDir)
if (-not $RunDir) { $RunDir = Join-Path $PSScriptRoot ("runs/" + (Get-Date -Format "yyyyMMdd-HHmmss-fff")) }
& python (Join-Path $PSScriptRoot 'autocompiler.py') --config (Join-Path $PSScriptRoot 'config.json') --objective $Objective --run-dir $RunDir
exit $LASTEXITCODE
