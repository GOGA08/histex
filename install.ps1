#Requires -Version 5.1
<#
  histex one-shot installer (Windows).

  Run from the histex folder:
      powershell -ExecutionPolicy Bypass -File .\install.ps1

  What it does:
    1) checks fzf and tldr (install hints when missing)
    2) builds histex.exe when it is missing and Go is available
    3) runs `tldr --update` once (so explanations work offline)
    4) writes %APPDATA%\histex\config.json when there is none
    5) runs `histex.exe --doctor` and `--self-test`
  It never edits $PROFILE - that stays your decision.
#>
$ErrorActionPreference = 'Stop'

function Test-Tool($Name) {
    return [bool](Get-Command $Name -ErrorAction SilentlyContinue)
}

Write-Output "histex installer"
Write-Output ""

$failed = $false

if (Test-Tool fzf) {
    Write-Output ("[ok] fzf " + (fzf --version 2>&1))
} else {
    Write-Output "[x] fzf not found - run:  winget install junegunn.fzf"
    Write-Output "    then open a NEW terminal so PATH is refreshed."
    $failed = $true
}

if (Test-Tool tldr) {
    Write-Output "[ok] tldr found - refreshing the local pages..."
    tldr --update
} else {
    Write-Output "[i] tldr not found (optional but recommended) - run:  winget install dbrgn.tealdeer"
    Write-Output "    then:  tldr --update"
}

$exe = Join-Path $PSScriptRoot 'histex.exe'

if (-not (Test-Path $exe)) {
    if (Test-Tool go) {
        Write-Output "[..] building histex.exe ..."
        & go build -o $exe .
        if ($LASTEXITCODE -ne 0) {
            Write-Output "[x] go build failed"
            $failed = $true
        }
    } else {
        Write-Output "[x] histex.exe not found and go is not installed."
        Write-Output "    Build it with Go 1.27+ (go build -o histex.exe .) or copy a prebuilt histex.exe here."
        $failed = $true
    }
} else {
    Write-Output ("[ok] histex.exe (" + (Get-Item $exe).Length + " bytes)")
}

if ($failed) {
    Write-Output ""
    Write-Output "Fix the [x] lines above, open a new terminal, and run install.ps1 again."
    exit 1
}

Write-Output ""
Write-Output "[..] writing default config when missing..."
& $exe --init-config 2>$null | Out-Null
Write-Output ""
Write-Output "=== doctor ==="
& $exe --doctor
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
Write-Output ""
Write-Output "=== self-test ==="
& $exe --self-test
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
Write-Output ""
Write-Output "[ok] installed - run:  .\histex.exe"
Write-Output "     optional prompt integration:  .\histex.exe --install-snippets"
