param(
  [string]$Version = $env:FORMAL_GATES_VERSION,
  [ValidateSet("claude","codex","cursor","both")]
  [string]$Host = "claude",
  [ValidateSet("global","project")]
  [string]$Scope = "global",
  [string]$Project = "",
  [switch]$Force,
  [switch]$ConfigureHooks
)

$ErrorActionPreference = "Stop"
if (-not $Version) { $Version = "v0.1.0" }

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
  if (Test-Path $installRoot) { Remove-Item $installRoot -Recurse -Force }
  Copy-Item $sourceDir.FullName $installRoot -Recurse -Force

  $current = Join-Path $env:LOCALAPPDATA "formal-gates\current"
  New-Item -ItemType Directory -Force -Path (Split-Path $current) | Out-Null
  if (Test-Path $current) { Remove-Item $current -Recurse -Force }
  New-Item -ItemType SymbolicLink -Path $current -Target $installRoot | Out-Null

  $binDir = Join-Path $env:LOCALAPPDATA "formal-gates\bin"
  New-Item -ItemType Directory -Force -Path $binDir | Out-Null
  $formalBinary = Join-Path $binDir "formal-gates.exe"
  if (Test-Path $formalBinary) { Remove-Item $formalBinary -Force }
  New-Item -ItemType SymbolicLink -Path $formalBinary -Target (Join-Path $current "bin\formal-gates.exe") | Out-Null

  Write-Host "Installed formal-gates to $installRoot"
  Write-Host "Binary symlink: $formalBinary"
  if ($ConfigureHooks) {
    if ($Scope -eq "project" -and -not $Project) { throw "--project is required when --scope project is used" }
    $args = @("install", "--source", $installRoot, "--host", $Host, "--scope", $Scope)
    if ($Project) { $args += @("--project", $Project) }
    if ($Force) { $args += "--force" }
    $args += "--configure-hooks"
    & $formalBinary @args
  }
}
finally {
  Remove-Item $tmp -Recurse -Force
}
