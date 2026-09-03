# HyperTracker screener v2 — broader scan + analysis output
param(
    [string]$Token = $env:HT_TOKEN,
    [int]$WalletLimit = 200,
    [int]$LeaderLimit = 50
)

if (-not $Token) { Write-Error "HT_TOKEN required"; exit 1 }

$base = "https://ht-api.coinmarketman.com/api/external"
$h = @{ Authorization = "Bearer $Token"; Accept = "application/json" }

function Get-Json($Uri) {
    try { return Invoke-RestMethod -Uri $Uri -Headers $h -TimeoutSec 45 }
    catch { return $null }
}

$all = @{}

# Source A: wallets by perp PnL (with positions)
Write-Host "Fetching wallets (perpPnl, hasOpenPositions)..." -ForegroundColor Cyan
$w1 = Get-Json "$base/wallets?limit=$WalletLimit&orderBy=perpPnl&order=desc&hasOpenPositions=true"
if ($w1 -and $w1.items) { foreach ($x in $w1.items) { $all[$x.address] = $x } }

# Source B: wallets by perp equity (active traders)
Write-Host "Fetching wallets (perpEquity)..." -ForegroundColor Cyan
$w2 = Get-Json "$base/wallets?limit=$WalletLimit&orderBy=perpEquity&order=desc&hasOpenPositions=true"
if ($w2 -and $w2.items) { foreach ($x in $w2.items) { if (-not $all.ContainsKey($x.address)) { $all[$x.address] = $x } } }

# Source C: perp PnL leaderboard (month)
Write-Host "Fetching perp-pnl leaderboard (month)..." -ForegroundColor Cyan
$lb = Get-Json "$base/leaderboards/perp-pnl?limit=$LeaderLimit&rankBy=pnlMonth&orderBy=pnlMonth&order=desc"
if ($lb -and $lb.items) {
    foreach ($x in $lb.items) {
        if (-not $all.ContainsKey($x.address)) {
            $all[$x.address] = [PSCustomObject]@{
                address = $x.address
                perpEquity = $x.perpEquity
                perpPnl = $x.pnlMonth
                countOpenPositions = $x.openValue
                exposureRatio = $x.exposureRatio
                segments = $x.segments
            }
        }
    }
}

Write-Host "Unique candidates: $($all.Count)" -ForegroundColor Green

$rows = @()
$n = 0
foreach ($c in $all.Values) {
    $n++
    if ($n % 20 -eq 0) { Write-Host "  screened $n / $($all.Count)..." }
    Start-Sleep -Milliseconds 180
    $sum = Get-Json "$base/closed-trades/summary?address=$($c.address)&interval=90d"
    if (-not $sum -or -not $sum.summary) { continue }
    $s = $sum.summary
    if ($s.totalTrades -lt 5) { continue }

    $eq = 0; if ($null -ne $c.perpEquity) { $eq = [double]$c.perpEquity }
    $rows += [PSCustomObject]@{
        address       = $c.address
        perpEquity    = [math]::Round($eq, 0)
        trades90d     = $s.totalTrades
        winRatePct    = [math]::Round(100 * [double]$s.winRate, 1)
        netPnl90d     = [math]::Round([double]$s.netPnl, 2)
        profitFactor  = [math]::Round([double]$s.profitFactor, 2)
        expectancy    = [math]::Round([double]$s.expectancy, 2)
        avgGain       = if ($null -ne $s.avgGain) { [math]::Round([double]$s.avgGain, 2) } else { 0 }
        avgLoss       = if ($null -ne $s.avgLoss) { [math]::Round([double]$s.avgLoss, 2) } else { 0 }
        exposure      = if ($null -ne $c.exposureRatio) { [math]::Round([double]$c.exposureRatio, 2) } else { $null }
        score         = [math]::Round([double]$s.winRate * [double]$s.profitFactor * [math]::Log10([math]::Max($s.totalTrades, 1)), 3)
    }
}

Write-Host "`n=== ALL WITH 5+ TRADES (90d): $($rows.Count) ===" -ForegroundColor Yellow
$rows | Sort-Object score -Descending | Select-Object -First 25 | Format-Table -AutoSize

Write-Host "`n=== TIER A: strict (55%+ WR, 20+ trades, PF>=1.5, netPnl>0) ===" -ForegroundColor Green
$tierA = $rows | Where-Object { $_.winRatePct -ge 55 -and $_.trades90d -ge 20 -and $_.profitFactor -ge 1.5 -and $_.netPnl90d -gt 0 } |
    Sort-Object winRatePct, profitFactor, netPnl90d -Descending
$tierA | Format-Table -AutoSize

Write-Host "`n=== TIER B: solid (50%+ WR, 15+ trades, PF>=1.2, netPnl>0, equity 100-80k) ===" -ForegroundColor Cyan
$tierB = $rows | Where-Object {
    $_.winRatePct -ge 50 -and $_.trades90d -ge 15 -and $_.profitFactor -ge 1.2 -and $_.netPnl90d -gt 0 -and
    $_.perpEquity -ge 100 -and $_.perpEquity -le 80000
} | Sort-Object score -Descending
$tierB | Select-Object -First 15 | Format-Table -AutoSize

$inspect = @()
if ($tierA.Count -gt 0) { $inspect = $tierA | Select-Object -First 5 }
elseif ($tierB.Count -gt 0) { $inspect = $tierB | Select-Object -First 5 }
else { $inspect = $rows | Sort-Object score -Descending | Select-Object -First 5 }

Write-Host "`n=== LIVE POSITIONS (top picks) ===" -ForegroundColor Magenta
foreach ($t in $inspect) {
    Start-Sleep -Milliseconds 250
    $pos = Get-Json "$base/positions?address=$($t.address)"
    Write-Host "`n$($t.address) | WR=$($t.winRatePct)% PF=$($t.profitFactor) trades=$($t.trades90d) net90d=`$$($t.netPnl90d) equity=`$$($t.perpEquity)"
    if ($pos -and $pos.positions -and $pos.positions.Count -gt 0) {
        foreach ($p in $pos.positions) {
            Write-Host "  -> $($p.coin) szi=$($p.szi) entry=$($p.entryPx) uPnL=$($p.unrealizedPnl)"
        }
    } elseif ($pos -and $pos.items) {
        foreach ($p in $pos.items) { Write-Host "  -> $($p | ConvertTo-Json -Compress)" }
    } else {
        Write-Host "  -> $($pos | ConvertTo-Json -Depth 3 -Compress)"
    }
}

# Export JSON for local review
$outPath = Join-Path $PSScriptRoot "hypertracker_screener_results.json"
@{
    scannedAt = (Get-Date).ToUniversalTime().ToString("o")
    tierA = $tierA
    tierB = ($tierB | Select-Object -First 20)
    top25byScore = ($rows | Sort-Object score -Descending | Select-Object -First 25)
} | ConvertTo-Json -Depth 5 | Set-Content -Path $outPath -Encoding UTF8
Write-Host "`nSaved: $outPath" -ForegroundColor Gray
