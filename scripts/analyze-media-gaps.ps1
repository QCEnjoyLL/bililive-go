param(
    [Parameter(Mandatory = $true, Position = 0)]
    [string]$Path,

    [double]$ThresholdSeconds = 0.5,

    [int]$Top = 20,

    [string]$FFProbePath = ""
)

$ErrorActionPreference = "Stop"
$OutputEncoding = [System.Text.UTF8Encoding]::new($false)
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)

function Resolve-FFProbe {
    param([string]$ExplicitPath)

    if ($ExplicitPath) {
        return (Get-Item -LiteralPath $ExplicitPath).FullName
    }

    $cmd = Get-Command ffprobe -ErrorAction SilentlyContinue
    if ($cmd) {
        return $cmd.Source
    }

    $ffmpeg = Get-Command ffmpeg -ErrorAction SilentlyContinue
    if ($ffmpeg) {
        $candidate = Join-Path (Split-Path -Parent $ffmpeg.Source) "ffprobe.exe"
        if (Test-Path -LiteralPath $candidate) {
            return (Get-Item -LiteralPath $candidate).FullName
        }
    }

    $roots = New-Object System.Collections.Generic.List[string]
    $cwd = (Get-Location).Path
    $roots.Add($cwd)
    if ($PSScriptRoot) {
        $roots.Add((Split-Path -Parent $PSScriptRoot))
    }

    foreach ($root in ($roots | Select-Object -Unique)) {
        $found = Get-ChildItem -LiteralPath $root -Recurse -Filter ffprobe.exe -File -ErrorAction SilentlyContinue |
            Select-Object -First 1
        if ($found) {
            return $found.FullName
        }
    }

    throw "ffprobe not found. Install FFmpeg or pass -FFProbePath."
}

function Parse-DoubleOrNull {
    param($Value)

    if ($null -eq $Value) {
        return $null
    }
    $text = [string]$Value
    if ($text -eq "" -or $text -eq "N/A") {
        return $null
    }
    try {
        return [double]::Parse($text, [System.Globalization.CultureInfo]::InvariantCulture)
    } catch {
        return $null
    }
}

function Get-Median {
    param([double[]]$Values)

    if (!$Values -or $Values.Count -eq 0) {
        return 0.0
    }
    $sorted = $Values | Sort-Object
    $middle = [int]($sorted.Count / 2)
    if ($sorted.Count % 2 -eq 1) {
        return [double]$sorted[$middle]
    }
    return ([double]$sorted[$middle - 1] + [double]$sorted[$middle]) / 2.0
}

function Read-FrameTimes {
    param(
        [string]$Selector,
        [string]$FilePath,
        [string]$ProbePath
    )

    $jsonText = & $ProbePath -v error -select_streams $Selector `
        -show_entries frame=best_effort_timestamp_time,pkt_duration_time `
        -of json "$FilePath"

    if ($LASTEXITCODE -ne 0) {
        throw "ffprobe failed for stream $Selector."
    }
    if (!$jsonText) {
        return @()
    }

    $json = $jsonText | ConvertFrom-Json
    if (!$json.frames) {
        return @()
    }

    $frames = @()
    foreach ($frame in $json.frames) {
        $time = Parse-DoubleOrNull $frame.best_effort_timestamp_time
        if ($null -eq $time) {
            continue
        }
        $duration = Parse-DoubleOrNull $frame.pkt_duration_time
        $frames += [pscustomobject]@{
            Time     = [double]$time
            Duration = $duration
        }
    }
    return @($frames | Sort-Object Time)
}

function Analyze-Stream {
    param(
        [string]$Name,
        [string]$Selector,
        [string]$FilePath,
        [string]$ProbePath,
        [double]$Threshold,
        [int]$Limit
    )

    $frames = Read-FrameTimes -Selector $Selector -FilePath $FilePath -ProbePath $ProbePath
    if ($frames.Count -lt 2) {
        return [pscustomobject]@{
            Name       = $Name
            FrameCount = $frames.Count
            MedianStep = 0.0
            Gaps       = @()
            TotalGap   = 0.0
            MaxGap     = 0.0
        }
    }

    $deltas = New-Object System.Collections.Generic.List[double]
    for ($i = 1; $i -lt $frames.Count; $i++) {
        $delta = [double]$frames[$i].Time - [double]$frames[$i - 1].Time
        if ($delta -gt 0 -and $delta -lt 1.0) {
            $deltas.Add($delta)
        }
    }
    $medianStep = Get-Median $deltas.ToArray()
    if ($medianStep -le 0) {
        $medianStep = 0.0
    }

    $gaps = New-Object System.Collections.Generic.List[object]
    for ($i = 1; $i -lt $frames.Count; $i++) {
        $prev = $frames[$i - 1]
        $curr = $frames[$i]
        $expected = $prev.Duration
        if ($null -eq $expected -or $expected -le 0 -or $expected -gt 1.0) {
            $expected = $medianStep
        }
        $delta = [double]$curr.Time - [double]$prev.Time
        $missing = $delta - [double]$expected
        if ($missing -ge $Threshold) {
            $gaps.Add([pscustomobject]@{
                From    = [double]$prev.Time
                To      = [double]$curr.Time
                Delta   = [double]$delta
                Missing = [double]$missing
            })
        }
    }

    $totalGap = 0.0
    $maxGap = 0.0
    foreach ($gap in $gaps) {
        $totalGap += $gap.Missing
        if ($gap.Missing -gt $maxGap) {
            $maxGap = $gap.Missing
        }
    }

    return [pscustomobject]@{
        Name       = $Name
        FrameCount = $frames.Count
        MedianStep = $medianStep
        Gaps       = @($gaps | Sort-Object Missing -Descending | Select-Object -First $Limit)
        TotalGap   = $totalGap
        MaxGap     = $maxGap
        GapCount   = $gaps.Count
    }
}

function Format-Seconds {
    param([double]$Seconds)
    return ("{0:N3}s" -f $Seconds)
}

$mediaFile = (Get-Item -LiteralPath $Path).FullName
$probe = Resolve-FFProbe -ExplicitPath $FFProbePath

Write-Host "File: $mediaFile"
Write-Host "ffprobe: $probe"
Write-Host ("Threshold: >= {0}" -f (Format-Seconds $ThresholdSeconds))
Write-Host ""

$video = Analyze-Stream -Name "video" -Selector "v:0" -FilePath $mediaFile -ProbePath $probe -Threshold $ThresholdSeconds -Limit $Top
$audio = Analyze-Stream -Name "audio" -Selector "a:0" -FilePath $mediaFile -ProbePath $probe -Threshold $ThresholdSeconds -Limit $Top

foreach ($result in @($video, $audio)) {
    Write-Host ("[{0}] samples: {1}, median step: {2}, gaps: {3}, total missing: {4}, max missing: {5}" -f `
        $result.Name, $result.FrameCount, (Format-Seconds $result.MedianStep), $result.GapCount, (Format-Seconds $result.TotalGap), (Format-Seconds $result.MaxGap))

    if ($result.Gaps.Count -gt 0) {
        $result.Gaps | ForEach-Object {
            Write-Host ("  {0} -> {1}, missing ~{2}, delta {3}" -f `
                (Format-Seconds $_.From), (Format-Seconds $_.To), (Format-Seconds $_.Missing), (Format-Seconds $_.Delta))
        }
    }
    Write-Host ""
}
