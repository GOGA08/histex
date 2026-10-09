# Packaging

Manifests for the package managers, so histex can be installed without a
browser download (which is also what keeps SmartScreen quiet - see the
"Verify the download" notes in the README).

## scoop (works today)

`packaging/scoop/histex.json` is pinned to the current release. Users install
straight from this file:

```powershell
scoop install https://raw.githubusercontent.com/GOGA08/histex/main/packaging/scoop/histex.json
```

`scoop install <url>` / `<path>` is a documented scoop feature, and the manifest
declares `"depends": "fzf"`, so scoop pulls fzf from the main bucket as well.

**On every release, bump two fields:**

1. `version` -> the new tag without the `v`
2. `hash` -> `(Get-FileHash .\histex.exe -Algorithm SHA256).Hash.ToLower()`
   for the `histex.exe` of that release

`checkver`/`autoupdate` are already set up, so `scoop checkver` (from the
`scoop` tooling) can do it automatically:

```powershell
scoop checkver packaging\scoop\histex.json -u
```

To test a manifest without installing anything:

```powershell
scoop download packaging\scoop\histex.json   # downloads and verifies the hash
```

## winget (manifests prepared, submission pending)

`packaging/winget/` holds the three manifest files for
[microsoft/winget-pkgs](https://github.com/microsoft/winget-pkgs). They follow
the `portable` installer type and declare `junegunn.fzf` as a dependency.

Submitting needs a pull request to that repository, which cannot be automated
from this machine (it needs a GitHub token with `public_repo` scope and the
interactive `wingetcreate` flow):

```powershell
winget install Microsoft.WingetCreate
wingetcreate submit --token <YOUR_PAT> packaging\winget\histex.yaml
```

Alternatively, copy the three files into a fork of `microsoft/winget-pkgs`
under `manifests/g/GOGA08/histex/<version>/` and open a pull request by hand.
A manifest whose URL and SHA256 match the release passes the automated checks;
a human reviewer then looks at it before it lands in the community repo.

As with scoop, `PackageVersion` and `InstallerSha256` have to be bumped for
every release.
