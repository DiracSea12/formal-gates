# Windows installer for formal-gates.
#
# The bootstrap keeps installed releases and the native executable as regular
# copied files. No elevated symbolic-link privilege or Developer Mode is needed.

param(
  [string]$Version = $env:FORMAL_GATES_VERSION,
  [Alias("Host")]
  [string]$TargetHost = "claude",
  [ValidateSet("global","project")]
  [string]$Scope = "global",
  [string]$Project = "",
  [switch]$Force,
  [switch]$SkipHooks
)

$ErrorActionPreference = "Stop"
if (-not $Version) { $Version = "v0.1.0" }
$stagedLauncher = $false

function Test-LegacyOwnerRejectedNewFlags([string]$Output) {
  return $Output -match "flag provided but not defined|unknown flag|unknown option"
}

$repo = "DiracSea12/formal-gates"
$os = "windows"
$arch = "amd64"
if ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture -ne [System.Runtime.InteropServices.Architecture]::X64) {
  $arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()
}
$suffix = "$os-$arch"
if ($suffix -ne "windows-amd64") { throw "unsupported release platform: $suffix" }
$asset = "formal-gates-$suffix.exe"
$canary = "portable-canary-$suffix.json"
$checksums = "SHA256SUMS-$suffix.txt"

$tmp = Join-Path $env:TEMP ("formal-gates-" + [guid]::NewGuid().ToString())
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
  $release = Invoke-RestMethod "https://api.github.com/repos/$repo/releases/tags/$Version"
  $assetUrl = ($release.assets | Where-Object { $_.name -eq $asset } | Select-Object -First 1).browser_download_url
  if (-not $assetUrl) { throw "missing release asset: $asset" }

  $sourceZip = Join-Path $tmp "source.zip"
  Invoke-WebRequest "https://api.github.com/repos/$repo/zipball/$Version" -OutFile $sourceZip
  Invoke-WebRequest $assetUrl -OutFile (Join-Path $tmp $asset)
  Invoke-WebRequest "https://github.com/$repo/releases/download/$Version/$canary" -OutFile (Join-Path $tmp $canary)
  Invoke-WebRequest "https://github.com/$repo/releases/download/$Version/$checksums" -OutFile (Join-Path $tmp $checksums)

  $lines = Get-Content (Join-Path $tmp $checksums)
  foreach ($file in @($asset, $canary)) {
    $check = (Get-FileHash -Algorithm SHA256 (Join-Path $tmp $file)).Hash.ToLower() + "  " + $file
    if (-not ($lines -contains $check)) { throw "checksum validation failed: $file" }
  }

  $sourceRoot = Join-Path $tmp "source"
  Expand-Archive -Path $sourceZip -DestinationPath $sourceRoot -Force
  $sourceDir = Get-ChildItem $sourceRoot | Where-Object { $_.PSIsContainer } | Select-Object -First 1
  if (-not $sourceDir) { throw "failed to unpack source zip" }
  New-Item -ItemType Directory -Force -Path (Join-Path $sourceDir.FullName "bin") | Out-Null
  Copy-Item (Join-Path $tmp $asset) (Join-Path $sourceDir.FullName "bin\formal-gates.exe") -Force

  $installRoot = Join-Path $env:LOCALAPPDATA "formal-gates\releases\$($Version.TrimStart('v'))-$suffix"
  $formalBinary = Join-Path $env:LOCALAPPDATA "formal-gates\bin\formal-gates.exe"

  if ($Scope -eq "project" -and -not $Project) { throw "--project is required when --scope project is used" }
  # The stable launcher is the only native writer. An existing launcher is
  # never overwritten by this script; the native journal owns that replacement.
  # On a first install only, place the verified candidate at the empty stable
  # path so the same stable owner can create the journal and finish the work.
  $launcherDir = Split-Path -Parent $formalBinary
  New-Item -ItemType Directory -Force -Path $launcherDir | Out-Null
  if (-not (Test-Path -LiteralPath $formalBinary)) {
    Copy-Item (Join-Path $sourceDir.FullName "bin\formal-gates.exe") $formalBinary -Force
    $stagedLauncher = $true
  }
  $args = @("install", "--source", $sourceDir.FullName, "--release-root", $installRoot, "--binary-target", $formalBinary, "--host", $TargetHost, "--scope", $Scope)
  if ($Project) { $args += @("--project", $Project) }
  if ($Force) { $args += "--force" }
  if ($SkipHooks) { $args += "--skip-hooks" }
  $ownerOutput = & $formalBinary @args 2>&1
  if ($LASTEXITCODE -ne 0) {
    $ownerText = ($ownerOutput | Out-String)
    if (-not (Test-LegacyOwnerRejectedNewFlags $ownerText)) {
      throw "formal-gates install failed with exit code $LASTEXITCODE"
    }
    # A pre-contract launcher can reject the new owner flags before its
    # transaction starts. Replace it only for this compatibility handoff,
    # then retry with the verified current binary at the fixed path. When the
    # legacy launcher is a link (any reparse point), remove it before copying
    # so the retry writes a real file instead of writing through the link
    # into the old release. On failure the backed-up bytes are restored as a
    # real file: the fixed launcher path keeps launching the same legacy
    # binary while also satisfying the immutable-real-file pointer invariant.
    $launcherWasLink = [bool](Get-Item -LiteralPath $formalBinary -Force).LinkType
    $launcherBackup = Join-Path $tmp "launcher.before.exe"
    Copy-Item $formalBinary $launcherBackup -Force
    if ($launcherWasLink) {
      Remove-Item -LiteralPath $formalBinary -Force
    }
    Copy-Item (Join-Path $sourceDir.FullName "bin\formal-gates.exe") $formalBinary -Force
    $stagedLauncher = $true
    & $formalBinary @args
    if ($LASTEXITCODE -ne 0) {
      Copy-Item $launcherBackup $formalBinary -Force
      $stagedLauncher = $false
      throw "formal-gates install failed with exit code $LASTEXITCODE"
    }
  }
  $stagedLauncher = $false
  # Bootstrap the installed artifact through the same stable launcher before
  # any workflow command can write state.
  $bootstrapArgs = @("install", "--bootstrap", "--source", $installRoot, "--release-root", $installRoot, "--binary-target", $formalBinary, "--host", $TargetHost, "--scope", $Scope)
  if ($Project) { $bootstrapArgs += @("--project", $Project) }
  & $formalBinary @bootstrapArgs
  if ($LASTEXITCODE -ne 0) { throw "formal-gates bootstrap failed with exit code $LASTEXITCODE" }

  Write-Host "Installed formal-gates to $installRoot"
  Write-Host "Native binary: $formalBinary"
}
finally {
  if ($stagedLauncher -and (Test-Path -LiteralPath $formalBinary)) {
    Remove-Item -LiteralPath $formalBinary -Force
  }
  Remove-Item $tmp -Recurse -Force
}
