# HyperTracker 3-step wallet screener (local analysis only)
param(
    [string]$Token = $env:HT_TOKEN,
    [int]$CandidateLimit = 40,
    [double]$MinEquity = 500,
    [double]$MaxEquity = 50000,
    [double]$MinWinRate = 0.55,
    [int]$MinTrades = 30,
    [double]$MinProfitFactor = 1.5
)

if (-not $Token) {
    Write-Error "Set HT_TOKEN env var or pass -Token"
    exit 1
}

$base = "https://ht-api.coinmarketman.com/api/external"
$headers = @{
    Authorization = "Bearer $Token"
    Accept        = "application/json"
}

function Get-Json($Uri) {
    try {
        return Invoke-RestMethod -Uri $Uri -Headers $headers -TimeoutSec 30
    } catch {
        Write-Warning "FAIL $Uri : $($_.Exception.Message)"
        return $null
    }
}

Write-Host "=== Step 1: Fetch perp-PnL candidates (equity $MinEquity - $MaxEquity) ===" -ForegroundColor Cyan
$wallets = Get-Json "$base/wallets?limit=$CandidateLimit&orderBy=perpPnl&order=desc&hasOpenPositions=true"
if (-not $wallets -or -not $wallets.items) {
    Write-Error "No wallet data"
    exit 1
}

$candidates = $wallets.items | Where-Object {
    $_.perpEquity -ge $MinEquity -and $_.perpEquity -le $MaxEquity
}
Write-Host "Candidates after equity filter: $($candidates.Count) / $($wallets.items.Count)"

Write-Host "`n=== Step 2: 90d closed-trades summary + filters ===" -ForegroundColor Cyan
$passed = @()
$i = 0
foreach ($c in $candidates) {
    $i++
    Start-Sleep -Milliseconds 250
    $addr = $c.address
    $sum = Get-Json "$base/closed-trades/summary?address=$addr&interval=90d"
    if (-not $sum -or -not $sum.summary) { continue }
    $s = $sum.summary
    if ($s.totalTrades -lt $MinTrades) { continue }
    if ($s.winRate -lt $MinWinRate) { continue }
    if ($s.netPnl -le 0) { continue }
    if ($s.profitFactor -lt $MinProfitFactor) { continue }

    $passed += [PSCustomObject]@{
        address       = $addr
        perpEquity    = [math]::Round([double]$c.perpEquity, 0)
        perpPnl       = [math]::Round([double]$c.perpPnl, 0)
        openPos       = $c.countOpenPositions
        exposureRatio = if ($null -ne $c.exposureRatio) { [math]::Round([double]$c.exposureRatio, 2) } else { $null }
        trades90d     = $s.totalTrades
        winRatePct    = [math]::Round(100 * [double]$s.winRate, 1)
        netPnl90d     = [math]::Round([double]$s.netPnl, 2)
        profitFactor  = [math]::Round([double]$s.profitFactor, 2)
        expectancy    = [math]::Round([double]$s.expectancy, 2)
        avgGain       = if ($null -ne $s.avgGain) { [math]::Round([double]$s.avgGain, 2) } else { $null }
        avgLoss       = if ($null -ne $s.avgLoss) { [math]::Round([double]$s.avgLoss, 2) } else { $null }
        segments      = ($c.segments -join ",")
    }
    Write-Host "  PASS [$i] $addr win=$($passed[-1].winRatePct)% PF=$($passed[-1].profitFactor) trades=$($passed[-1].trades90d)"
}

if ($passed.Count -eq 0) {
    Write-Host "`nNo wallets passed strict filters. Relaxing min trades to 20..." -ForegroundColor Yellow
    $MinTrades = 20
    foreach ($c in $candidates) {
        Start-Sleep -Milliseconds 250
        $addr = $c.address
        $sum = Get-Json "$base/closed-trades/summary?address=$addr&interval=90d"
        if (-not $sum -or -not $sum.summary) { continue }
        $s = $sum.summary
        if ($s.totalTrades -lt $MinTrades) { continue }
        if ($s.winRate -lt $MinWinRate) { continue }
        if ($s.netPnl -le 0) { continue }
        if ($s.profitFactor -lt $MinProfitFactor) { continue }
        $passed += [PSCustomObject]@{
            address       = $addr
            perpEquity    = [math]::Round([double]$c.perpEquity, 0)
            perpPnl       = [math]::Round([double]$c.perpPnl, 0)
            openPos       = $c.countOpenPositions
            exposureRatio = if ($null -ne $c.exposureRatio) { [math]::Round([double]$c.exposureRatio, 2) } else { $null }
            trades90d     = $s.totalTrades
            winRatePct    = [math]::Round(100 * [double]$s.winRate, 1)
            netPnl90d     = [math]::Round([double]$s.netPnl, 2)
            profitFactor  = [math]::Round([double]$s.profitFactor, 2)
            expectancy    = [math]::Round([double]$s.expectancy, 2)
            avgGain       = if ($null -ne $s.avgGain) { [math]::Round([double]$s.avgGain, 2) } else { $null }
            avgLoss       = if ($null -ne $s.avgLoss) { [math]::Round([double]$s.avgLoss, 2) } else { $null }
            segments      = ($c.segments -join ",")
        }
    }
}

$ranked = $passed | Sort-Object @{Expression='winRatePct'; Descending=$true}, @{Expression='profitFactor'; Descending=$true}, @{Expression='netPnl90d'; Descending=$true}
Write-Host "`n=== RANKED ($($ranked.Count) wallets passed) ===" -ForegroundColor Green
$ranked | Format-Table -AutoSize

$top = $ranked | Select-Object -First 5
Write-Host "`n=== Step 3: Live positions (top $($top.Count)) ===" -ForegroundColor Cyan
foreach ($t in $top) {
    Start-Sleep -Milliseconds 300
    $pos = Get-Json "$base/positions?address=$($t.address)"
    Write-Host "`n--- $($t.address) | win=$($t.winRatePct)% PF=$($t.profitFactor) equity=`$$($t.perpEquity) ---" -ForegroundColor Yellow
    if ($pos -and $pos.positions) {
        $pos.positions | ForEach-Object {
            $side = if ($_.side) { $_.side } elseif ($_.szi -gt 0) { "long" } else { "short" }
            $sz = if ($_.size) { $_.size } else { $_.szi }
            Write-Host "  $($_.coin) $side size=$sz entry=$($_.entryPx) uPnL=$($_.unrealizedPnl) lev=$($_.leverage)"
        }
    } else {
        Write-Host "  (no position detail or empty)"
        if ($pos) { $pos | ConvertTo-Json -Depth 4 -Compress | Write-Host }
    }
}

Write-Host "`nDone. Review ranked table above for copy-trade candidates (Hyperliquid / Autopilot only)." -ForegroundColor Green
