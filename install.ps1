#Requires -Version 5.1
<#
  histex one-shot installer (Windows).

  Run from the histex folder:
      powershell -ExecutionPolicy Bypass -File .\install.ps1

  What it does:
    1) checks python, fzf and tldr (install hints when missing)
    2) runs `tldr --update` once (so explanations work offline)
    3) writes ~/.histex/config.json when there is none
    4) runs `python histex.py --doctor` and `--self-test`
  It never edits $PROFILE - that stays your decision.
#>
$ErrorActionPreference = 'Stop'

function Test-Tool($Name) {
    return [bool](Get-Command $Name -ErrorAction SilentlyContinue)
}

Write-Output "histex installer"
Write-Output ""

$failed = $false

if (Test-Tool python) {
    Write-Output ("[ok] " + (python --version 2>&1))
} else {
    Write-Output "[x] python not found - install Python 3.8+ from https://www.python.org/downloads/"
    $failed = $true
}

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

if ($failed) {
    Write-Output ""
    Write-Output "Fix the [x] lines above, open a new terminal, and run install.ps1 again."
    exit 1
}

Write-Output ""
Write-Output "[..] writing default config when missing..."
python histex.py --init-config 2>$null | Out-Null
Write-Output ""
Write-Output "=== doctor ==="
python histex.py --doctor
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
Write-Output ""
Write-Output "=== self-test ==="
python histex.py --self-test
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
Write-Output ""
Write-Output "[ok] installed - run:  python histex.py"
Write-Output "     optional prompt integration:  python histex.py --install-snippets"
