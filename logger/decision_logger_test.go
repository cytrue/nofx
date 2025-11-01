
package logger

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// createTestLogFile is a helper function to create a temporary log file for testing.
func createTestLogFile(t *testing.T, dir, filename string, data []byte) {
	t.Helper()
	path := filepath.Join(dir, filename)
	if err := ioutil.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("Failed to write test log file %s: %v", filename, err)
	}
}

func TestAnalyzePerformance(t *testing.T) {
	// 1. Setup: Create a temporary directory for logs
	logDir, err := ioutil.TempDir("", "test_logs_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(logDir)

	// 2. Test Data: Define a series of mock decision records
	//    - Trade 1: Successful Long Trade on BTC
	//    - Trade 2: Losing Short Trade on ETH (hit Stop-Loss)
	
	// --- Mock Data Definitions ---
	openTimeBTC := time.Now().Add(-1 * time.Hour)
	closeTimeBTC := time.Now().Add(-30 * time.Minute)
	openTimeETH := time.Now().Add(-20 * time.Minute)
	closeTimeETH := time.Now().Add(-10 * time.Minute)

	// Record 1: AI decides to open a long position on BTC
	btcOpenDecisionJSON := `[{"symbol": "BTCUSDT", "action": "open_long", "leverage": 10, "position_size_usd": 1000, "stop_loss": 60000, "take_profit": 62000}]`
	btcOpenRecord := DecisionRecord{
		Timestamp:    openTimeBTC,
		DecisionJSON: btcOpenDecisionJSON,
		Decisions: []DecisionAction{
			{Action: "open_long", Symbol: "BTCUSDT", Quantity: 0.0166, Leverage: 10, Price: 60100, Timestamp: openTimeBTC, Success: true},
		},
		MarketData: map[string]MarketDataSnapshot{
			"BTCUSDT": {CurrentPrice: 60100, CurrentVWAP: 60000, CurrentRSI7: 55, CurrentMACD: 10},
		},
	}

	// Record 2: AI decides to close the BTC position for a profit
	btcCloseRecord := DecisionRecord{
		Timestamp: closeTimeBTC,
		Decisions: []DecisionAction{
			{Action: "close_long", Symbol: "BTCUSDT", Quantity: 0.0166, Price: 61000, Timestamp: closeTimeBTC, Success: true},
		},
	}

	// Record 3: AI decides to open a short position on ETH
	ethOpenDecisionJSON := `[{"symbol": "ETHUSDT", "action": "open_short", "leverage": 20, "position_size_usd": 500, "stop_loss": 3050, "take_profit": 2950}]`
	ethOpenRecord := DecisionRecord{
		Timestamp:    openTimeETH,
		DecisionJSON: ethOpenDecisionJSON,
		Decisions: []DecisionAction{
			{Action: "open_short", Symbol: "ETHUSDT", Quantity: 0.165, Leverage: 20, Price: 3020, Timestamp: openTimeETH, Success: true},
		},
		MarketData: map[string]MarketDataSnapshot{
			"ETHUSDT": {CurrentPrice: 3020, CurrentVWAP: 3030, CurrentRSI7: 45, CurrentMACD: -5},
		},
	}

	// Record 4: AI decides to close the ETH position, simulating a stop-loss hit
	ethCloseRecord := DecisionRecord{
		Timestamp: closeTimeETH,
		Decisions: []DecisionAction{
			// The close price is at the stop-loss defined in the open decision
			{Action: "close_short", Symbol: "ETHUSDT", Quantity: 0.165, Price: 3050, Timestamp: closeTimeETH, Success: true},
		},
	}

	// 3. Create mock log files in chronological order
	records := []struct {
		name   string
		record DecisionRecord
	}{
		{"log_01_btc_open.json", btcOpenRecord},
		{"log_02_btc_close.json", btcCloseRecord},
		{"log_03_eth_open.json", ethOpenRecord},
		{"log_04_eth_close.json", ethCloseRecord},
	}

	for _, r := range records {
		data, _ := json.Marshal(r.record)
		createTestLogFile(t, logDir, r.name, data)
		// Sleep to ensure file modification times are sequential
		time.Sleep(10 * time.Millisecond)
	}

	// 4. Run the function to be tested
	logger := NewDecisionLogger(logDir)
	analysis, err := logger.AnalyzePerformance(10) // Look back at 10 records

	// 5. Assertions: Check if the analysis is correct
	if err != nil {
		t.Fatalf("AnalyzePerformance failed: %v", err)
	}

	if analysis.TotalTrades != 2 {
		t.Errorf("Expected TotalTrades to be 2, but got %d", analysis.TotalTrades)
	}
	if analysis.WinningTrades != 1 {
		t.Errorf("Expected WinningTrades to be 1, but got %d", analysis.WinningTrades)
	}
	if analysis.LosingTrades != 1 {
		t.Errorf("Expected LosingTrades to be 1, but got %d", analysis.LosingTrades)
	}
	if analysis.WinRate != 50.0 {
		t.Errorf("Expected WinRate to be 50.0, but got %.2f", analysis.WinRate)
	}

	// Check details of the first trade (ETH, the most recent one)
	if len(analysis.RecentTrades) != 2 {
		t.Fatalf("Expected 2 recent trades, but got %d", len(analysis.RecentTrades))
	}
	
	ethTrade := analysis.RecentTrades[0]
	if ethTrade.Symbol != "ETHUSDT" {
		t.Errorf("Expected most recent trade to be ETHUSDT, but got %s", ethTrade.Symbol)
	}
	if ethTrade.CloseReason != "SL" {
		t.Errorf("Expected ETH trade CloseReason to be 'SL', but got '%s'", ethTrade.CloseReason)
	}
	if ethTrade.PnL >= 0 {
		t.Errorf("Expected ETH trade PnL to be negative, but got %.2f", ethTrade.PnL)
	}
	expectedEthPnl := 0.165 * (3020 - 3050) // quantity * (openPrice - closePrice)
	if (ethTrade.PnL - expectedEthPnl) > 0.001 {
		t.Errorf("Expected ETH PnL to be around %.4f, but got %.4f", expectedEthPnl, ethTrade.PnL)
	}


	// Check details of the second trade (BTC)
	btcTrade := analysis.RecentTrades[1]
	if btcTrade.Symbol != "BTCUSDT" {
		t.Errorf("Expected second trade to be BTCUSDT, but got %s", btcTrade.Symbol)
	}
	if btcTrade.CloseReason != "Strategy" {
		t.Errorf("Expected BTC trade CloseReason to be 'Strategy', but got '%s'", btcTrade.CloseReason)
	}
	if btcTrade.PnL <= 0 {
		t.Errorf("Expected BTC trade PnL to be positive, but got %.2f", btcTrade.PnL)
	}
	expectedBtcPnl := 0.0166 * (61000 - 60100) // quantity * (closePrice - openPrice)
	if (btcTrade.PnL - expectedBtcPnl) > 0.001 {
		t.Errorf("Expected BTC PnL to be around %.4f, but got %.4f", expectedBtcPnl, btcTrade.PnL)
	}
	if btcTrade.EntryVWAP != 60000 {
		t.Errorf("Expected BTC EntryVWAP to be 60000, but got %.2f", btcTrade.EntryVWAP)
	}
}
