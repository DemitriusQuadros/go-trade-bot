package futuresmath

import (
	"math"
	"testing"
	"time"
)

const floatTolerance = 1e-4

func almostEqual(a, b, tol float64) bool {
	return math.Abs(a-b) <= tol
}

func TestLookupBracketAndMaintenanceMargin(t *testing.T) {
	// BTC / ETH tests
	btcTests := []struct {
		notional float64
		wantTier int
		wantMMR  float64
		wantCum  float64
	}{
		{0, 1, 0.004, 0},
		{25_000, 1, 0.004, 0},
		{50_000, 1, 0.004, 0},
		{50_000.01, 2, 0.005, 50},
		{250_000, 2, 0.005, 50},
		{250_001, 3, 0.010, 1_300},
		{1_000_000, 3, 0.010, 1_300},
		{5_000_000, 4, 0.025, 16_300},
		{10_000_000, 4, 0.025, 16_300},
		{15_000_000, 5, 0.050, 266_300},
	}

	for _, tt := range btcTests {
		b := LookupBracket("BTCUSDT", tt.notional)
		if b.Tier != tt.wantTier {
			t.Errorf("BTCUSDT notional %f: got tier %d, want %d", tt.notional, b.Tier, tt.wantTier)
		}
		if b.MaintenanceMarginRate != tt.wantMMR {
			t.Errorf("BTCUSDT notional %f: got MMR %f, want %f", tt.notional, b.MaintenanceMarginRate, tt.wantMMR)
		}
		if b.MaintenanceAmount != tt.wantCum {
			t.Errorf("BTCUSDT notional %f: got Cum %f, want %f", tt.notional, b.MaintenanceAmount, tt.wantCum)
		}

		mm, mmB := MaintenanceMargin("ETHUSDT", tt.notional)
		if mmB.Tier != tt.wantTier {
			t.Errorf("ETHUSDT notional %f: got tier %d, want %d", tt.notional, mmB.Tier, tt.wantTier)
		}
		expectedMM := math.Max(0, tt.notional*tt.wantMMR-tt.wantCum)
		if !almostEqual(mm, expectedMM, floatTolerance) {
			t.Errorf("ETHUSDT notional %f: got mm %f, want %f", tt.notional, mm, expectedMM)
		}
	}

	// Altcoin tests
	altTests := []struct {
		notional float64
		wantTier int
		wantMMR  float64
		wantCum  float64
	}{
		{5_000, 1, 0.005, 0},
		{10_000, 1, 0.005, 0},
		{20_000, 2, 0.010, 50},
		{50_000, 2, 0.010, 50},
		{100_000, 3, 0.020, 550},
		{500_000, 4, 0.050, 8_050},
		{2_000_000, 5, 0.100, 58_050},
	}

	for _, tt := range altTests {
		b := LookupBracket("SOLUSDT", tt.notional)
		if b.Tier != tt.wantTier {
			t.Errorf("SOLUSDT notional %f: got tier %d, want %d", tt.notional, b.Tier, tt.wantTier)
		}
		mm, _ := MaintenanceMargin("SOLUSDT", tt.notional)
		expectedMM := math.Max(0, tt.notional*tt.wantMMR-tt.wantCum)
		if !almostEqual(mm, expectedMM, floatTolerance) {
			t.Errorf("SOLUSDT notional %f: got mm %f, want %f", tt.notional, mm, expectedMM)
		}
	}
}

func TestLiquidationPrice(t *testing.T) {
	// 1x Long with zero funding -> LiqPrice == 0
	liq1xLong := IsolatedLiquidationPrice("BTCUSDT", SideLong, 50_000, 1.0, 1.0, 0, 0)
	if liq1xLong != 0 {
		t.Errorf("1x Long with zero funding: got liqPrice %f, want 0", liq1xLong)
	}

	// 10x Long on BTC at 50,000 (qty = 1.0):
	// LiqPrice = (50000 - 5000) / (1 - 0.004) = 45000 / 0.996 = 45180.72289...
	expected10xLong := 45000.0 / 0.996
	liq10xLong := IsolatedLiquidationPrice("BTCUSDT", SideLong, 50_000, 1.0, 10.0, 0, 0)
	if !almostEqual(liq10xLong, expected10xLong, 1e-3) {
		t.Errorf("10x Long on BTC: got %f, want %f", liq10xLong, expected10xLong)
	}

	// 10x Short on BTC at 50,000 (qty = 1.0):
	// LiqPrice = (50000 + 5000) / (1 + 0.004) = 55000 / 1.004 = 54780.87649...
	expected10xShort := 55000.0 / 1.004
	liq10xShort := IsolatedLiquidationPrice("BTCUSDT", SideShort, 50_000, 1.0, 10.0, 0, 0)
	if !almostEqual(liq10xShort, expected10xShort, 1e-3) {
		t.Errorf("10x Short on BTC: got %f, want %f", liq10xShort, expected10xShort)
	}

	// Leverage variations: 20x, 50x, 125x Long & Short
	leverages := []float64{20, 50, 125}
	for _, lev := range leverages {
		longP := IsolatedLiquidationPrice("BTCUSDT", SideLong, 50_000, 0.5, lev, 0, 0)
		shortP := IsolatedLiquidationPrice("BTCUSDT", SideShort, 50_000, 0.5, lev, 0, 0)
		if longP <= 0 || longP >= 50_000 {
			t.Errorf("Lev %.0f Long liqPrice out of expected bounds: %f", lev, longP)
		}
		if shortP <= 50_000 {
			t.Errorf("Lev %.0f Short liqPrice out of expected bounds: %f", lev, shortP)
		}
	}

	// Verify positive accumulatedFunding moves Long LiqPrice UP (closer to entry)
	// and Short LiqPrice DOWN (closer to entry).
	liqLongWithFunding := IsolatedLiquidationPrice("BTCUSDT", SideLong, 50_000, 1.0, 10.0, 0, 200)
	if liqLongWithFunding <= liq10xLong {
		t.Errorf("Long with funding should have higher liqPrice: got %f, baseline %f", liqLongWithFunding, liq10xLong)
	}

	liqShortWithFunding := IsolatedLiquidationPrice("BTCUSDT", SideShort, 50_000, 1.0, 10.0, 0, 200)
	if liqShortWithFunding >= liq10xShort {
		t.Errorf("Short with funding should have lower liqPrice: got %f, baseline %f", liqShortWithFunding, liq10xShort)
	}

	// CrossLiquidationPrice verification
	crossLong := CrossLiquidationPrice("BTCUSDT", SideLong, 50_000, 1.0, 10.0, 1_000, 0)
	if crossLong >= liq10xLong {
		t.Errorf("Cross with extra balance should have lower liqPrice than isolated: got %f, iso %f", crossLong, liq10xLong)
	}

	// Edge cases: invalid entryPrice / qty
	if IsolatedLiquidationPrice("BTCUSDT", SideLong, 0, 1.0, 10, 0, 0) != 0 {
		t.Error("entryPrice=0 should return 0")
	}
	if IsolatedLiquidationPrice("BTCUSDT", SideLong, 50_000, 0, 10, 0, 0) != 0 {
		t.Error("qty=0 should return 0")
	}
	if CrossLiquidationPrice("BTCUSDT", SideLong, -100, 1.0, 10, 0, 0) != 0 {
		t.Error("entryPrice<0 should return 0")
	}
}

func TestIsLiquidated(t *testing.T) {
	// Long position: candleLow <= liqPrice triggers liquidation
	liqLong := 45000.0
	if !IsLiquidated(SideLong, liqLong, 48000, 44990) {
		t.Error("Long should be liquidated when candleLow (44990) <= liqPrice (45000)")
	}
	if !IsLiquidated(SideLong, liqLong, 48000, 45000) {
		t.Error("Long should be liquidated when candleLow touches liqPrice exactly")
	}
	if IsLiquidated(SideLong, liqLong, 48000, 45010) {
		t.Error("Long should NOT be liquidated when candleLow (45010) > liqPrice (45000)")
	}

	// Short position: candleHigh >= liqPrice triggers liquidation
	liqShort := 55000.0
	if !IsLiquidated(SideShort, liqShort, 55010, 52000) {
		t.Error("Short should be liquidated when candleHigh (55010) >= liqPrice (55000)")
	}
	if !IsLiquidated(SideShort, liqShort, 55000, 52000) {
		t.Error("Short should be liquidated when candleHigh touches liqPrice exactly")
	}
	if IsLiquidated(SideShort, liqShort, 54990, 52000) {
		t.Error("Short should NOT be liquidated when candleHigh (54990) < liqPrice (55000)")
	}

	// LiqPrice <= 0 should never trigger
	if IsLiquidated(SideLong, 0, 50000, 1000) {
		t.Error("liqPrice=0 should never trigger liquidation")
	}
	if IsLiquidated(SideShort, -10, 50000, 1000) {
		t.Error("liqPrice<0 should never trigger liquidation")
	}
}

func TestUnrealizedPnL(t *testing.T) {
	entry := 50_000.0
	qty := 2.0

	// Long: price rises -> profit
	pnlLongUp := UnrealizedPnL(SideLong, entry, 52_000, qty)
	if !almostEqual(pnlLongUp, 4000.0, floatTolerance) {
		t.Errorf("Long PnL up: got %f, want 4000", pnlLongUp)
	}

	// Long: price drops -> loss
	pnlLongDown := UnrealizedPnL(SideLong, entry, 48_000, qty)
	if !almostEqual(pnlLongDown, -4000.0, floatTolerance) {
		t.Errorf("Long PnL down: got %f, want -4000", pnlLongDown)
	}

	// Short: price rises -> loss
	pnlShortUp := UnrealizedPnL(SideShort, entry, 52_000, qty)
	if !almostEqual(pnlShortUp, -4000.0, floatTolerance) {
		t.Errorf("Short PnL up: got %f, want -4000", pnlShortUp)
	}

	// Short: price drops -> profit
	pnlShortDown := UnrealizedPnL(SideShort, entry, 48_000, qty)
	if !almostEqual(pnlShortDown, 4000.0, floatTolerance) {
		t.Errorf("Short PnL down: got %f, want 4000", pnlShortDown)
	}
}

func TestNotionalFeeAndFundingPayment(t *testing.T) {
	// NotionalFee: qty=1.5, fillPrice=50,000, feeBps=5.0 (0.05% taker)
	// Notional = 75,000. Fee = 75,000 * 0.0005 = 37.5
	fee := NotionalFee(1.5, 50_000, DefaultFuturesTakerFeeBps)
	if !almostEqual(fee, 37.5, floatTolerance) {
		t.Errorf("NotionalFee: got %f, want 37.5", fee)
	}

	// FundingPayment sign conventions:
	// Positive rate: Long pays (>0), Short receives (<0)
	qty := 2.0
	markPrice := 30_000.0
	positiveRate := 0.0001 // +0.01%
	longPay := FundingPayment(SideLong, qty, markPrice, positiveRate)
	shortReceive := FundingPayment(SideShort, qty, markPrice, positiveRate)

	if longPay <= 0 || !almostEqual(longPay, 6.0, floatTolerance) {
		t.Errorf("Long pays with positive funding rate: got %f, want 6.0", longPay)
	}
	if shortReceive >= 0 || !almostEqual(shortReceive, -6.0, floatTolerance) {
		t.Errorf("Short receives with positive funding rate: got %f, want -6.0", shortReceive)
	}

	// Negative rate: Long receives (<0), Short pays (>0)
	negativeRate := -0.0002 // -0.02%
	longReceive := FundingPayment(SideLong, qty, markPrice, negativeRate)
	shortPay := FundingPayment(SideShort, qty, markPrice, negativeRate)

	if longReceive >= 0 || !almostEqual(longReceive, -12.0, floatTolerance) {
		t.Errorf("Long receives with negative funding rate: got %f, want -12.0", longReceive)
	}
	if shortPay <= 0 || !almostEqual(shortPay, 12.0, floatTolerance) {
		t.Errorf("Short pays with negative funding rate: got %f, want 12.0", shortPay)
	}
}

func TestFundingTimestampsInWindow(t *testing.T) {
	// 1m window: 07:59:00 to 08:00:00 UTC -> exactly 1 timestamp (08:00:00)
	t1 := time.Date(2026, 11, 2, 7, 59, 0, 0, time.UTC)
	t2 := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)
	ts1m := FundingTimestampsInWindow(t1, t2)
	if len(ts1m) != 1 || !ts1m[0].Equal(t2) {
		t.Errorf("1m window crossing 08:00: got %v, want [%v]", ts1m, t2)
	}

	// 1m window: 08:00:00 to 08:01:00 UTC -> 0 timestamps (08:00 was fromExclusive)
	t3 := time.Date(2026, 11, 2, 8, 1, 0, 0, time.UTC)
	ts1mAfter := FundingTimestampsInWindow(t2, t3)
	if len(ts1mAfter) != 0 {
		t.Errorf("1m window after 08:00: got %v, want empty", ts1mAfter)
	}

	// 1h window: 07:30 to 08:30 UTC -> exactly 1 timestamp (08:00:00)
	t4 := time.Date(2026, 11, 2, 7, 30, 0, 0, time.UTC)
	t5 := time.Date(2026, 11, 2, 8, 30, 0, 0, time.UTC)
	ts1h := FundingTimestampsInWindow(t4, t5)
	if len(ts1h) != 1 || !ts1h[0].Equal(t2) {
		t.Errorf("1h window: got %v, want [%v]", ts1h, t2)
	}

	// 4h window: 02:00 to 06:00 UTC -> 0 timestamps
	t6 := time.Date(2026, 11, 2, 2, 0, 0, 0, time.UTC)
	t7 := time.Date(2026, 11, 2, 6, 0, 0, 0, time.UTC)
	ts4h := FundingTimestampsInWindow(t6, t7)
	if len(ts4h) != 0 {
		t.Errorf("4h window without boundary: got %v, want empty", ts4h)
	}

	// 24h window: 00:00:00 to 24:00:00 (next day 00:00:00)
	// strictly after 00:00:00, <= next day 00:00:00 -> exactly 3 timestamps (08:00, 16:00, next day 00:00)
	startDay := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	endDay := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	ts24h := FundingTimestampsInWindow(startDay, endDay)
	if len(ts24h) != 3 {
		t.Fatalf("24h window: got %d timestamps, want 3 (%v)", len(ts24h), ts24h)
	}
	expectedTS := []time.Time{
		time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 2, 16, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC),
	}
	for i, exp := range expectedTS {
		if !ts24h[i].Equal(exp) {
			t.Errorf("24h window index %d: got %v, want %v", i, ts24h[i], exp)
		}
	}
}
