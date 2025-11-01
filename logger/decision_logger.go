package logger

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DecisionRecord 决策记录
type DecisionRecord struct {
	Timestamp      time.Time          `json:"timestamp"`       // 决策时间
	CycleNumber    int                `json:"cycle_number"`    // 周期编号
	InputPrompt    string             `json:"input_prompt"`    // 发送给AI的输入prompt
	CoTTrace       string             `json:"cot_trace"`       // AI思维链（输出）
	ValidationTrace []string          `json:"validation_trace,omitempty"` // AI交叉验证日志
	DecisionJSON   string             `json:"decision_json"`   // 决策JSON
	AccountState   AccountSnapshot    `json:"account_state"`   // 账户状态快照
	Positions      []PositionSnapshot `json:"positions"`       // 持仓快照
	CandidateCoins []string           `json:"candidate_coins"` // 候选币种列表
	Decisions      []DecisionAction   `json:"decisions"`       // 执行的决策
	ExecutionLog   []string           `json:"execution_log"`   // 执行日志
	Success        bool               `json:"success"`         // 是否成功
	ErrorMessage   string             `json:"error_message"`   // 错误信息（如果有）
	MarketData     map[string]MarketDataSnapshot `json:"market_data"`     // 市场数据快照
}

// MarketDataSnapshot 市场数据快照（用于日志）
type MarketDataSnapshot struct {
	CurrentPrice float64 `json:"current_price"`
	CurrentVWAP  float64 `json:"current_vwap"`
	CurrentRSI7  float64 `json:"current_rsi7"`
	CurrentMACD  float64 `json:"current_macd"`
}

// AccountSnapshot 账户状态快照
type AccountSnapshot struct {
	TotalBalance          float64 `json:"total_balance"`
	AvailableBalance      float64 `json:"available_balance"`
	TotalUnrealizedProfit float64 `json:"total_unrealized_profit"`
	PositionCount         int     `json:"position_count"`
	MarginUsedPct         float64 `json:"margin_used_pct"`
}

// PositionSnapshot 持仓快照
type PositionSnapshot struct {
	Symbol           string  `json:"symbol"`
	Side             string  `json:"side"`
	PositionAmt      float64 `json:"position_amt"`
	EntryPrice       float64 `json:"entry_price"`
	MarkPrice        float64 `json:"mark_price"`
	UnrealizedProfit float64 `json:"unrealized_profit"`
	Leverage         float64 `json:"leverage"`
	LiquidationPrice float64 `json:"liquidation_price"`
}

// DecisionAction 决策动作
type DecisionAction struct {
	Action    string    `json:"action"`    // open_long, open_short, close_long, close_short
	Symbol    string    `json:"symbol"`    // 币种
	Quantity  float64   `json:"quantity"`  // 数量
	Leverage  int       `json:"leverage"`  // 杠杆（开仓时）
	Price     float64   `json:"price"`     // 执行价格
	OrderID   int64     `json:"order_id"`  // 订单ID
	Timestamp time.Time `json:"timestamp"` // 执行时间
	Success   bool      `json:"success"`   // 是否成功
	Error     string    `json:"error"`     // 错误信息
}

// DecisionLogger 决策日志记录器
type DecisionLogger struct {
	logDir      string
	cycleNumber int
}

// NewDecisionLogger 创建决策日志记录器
func NewDecisionLogger(logDir string) *DecisionLogger {
	if logDir == "" {
		logDir = "decision_logs"
	}

	// 确保日志目录存在
	if err := os.MkdirAll(logDir, 0755); err != nil {
		fmt.Printf("⚠ 创建日志目录失败: %v\n", err)
	}

	return &DecisionLogger{
		logDir:      logDir,
		cycleNumber: 0,
	}
}

// LogDecision 记录决策
func (l *DecisionLogger) LogDecision(record *DecisionRecord) error {
	l.cycleNumber++
	record.CycleNumber = l.cycleNumber
	record.Timestamp = time.Now()

	// 生成文件名：decision_YYYYMMDD_HHMMSS_cycleN.json
	filename := fmt.Sprintf("decision_%s_cycle%d.json",
		record.Timestamp.Format("20060102_150405"),
		record.CycleNumber)

	filepath := filepath.Join(l.logDir, filename)

	// 序列化为JSON（带缩进，方便阅读）
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化决策记录失败: %w", err)
	}

	// 写入文件
	if err := ioutil.WriteFile(filepath, data, 0644); err != nil {
		return fmt.Errorf("写入决策记录失败: %w", err)
	}

	fmt.Printf("📝 决策记录已保存: %s\n", filename)
	return nil
}

// GetLatestRecords 获取最近N条记录（按时间正序：从旧到新）
func (l *DecisionLogger) GetLatestRecords(n int) ([]*DecisionRecord, error) {
	files, err := ioutil.ReadDir(l.logDir)
	if err != nil {
		return nil, fmt.Errorf("读取日志目录失败: %w", err)
	}

	// 先按修改时间倒序收集（最新的在前）
	var records []*DecisionRecord
	count := 0
	for i := len(files) - 1; i >= 0 && count < n; i-- {
		file := files[i]
		if file.IsDir() {
			continue
		}

		filepath := filepath.Join(l.logDir, file.Name())
		data, err := ioutil.ReadFile(filepath)
		if err != nil {
			continue
		}

		var record DecisionRecord
		if err := json.Unmarshal(data, &record); err != nil {
			continue
		}

		records = append(records, &record)
		count++
	}

	// 反转数组，让时间从旧到新排列（用于图表显示）
	for i, j := 0, len(records)-1; i < j; i, j = i+1, j-1 {
		records[i], records[j] = records[j], records[i]
	}

	return records, nil
}

// GetRecordByDate 获取指定日期的所有记录
func (l *DecisionLogger) GetRecordByDate(date time.Time) ([]*DecisionRecord, error) {
	dateStr := date.Format("20060102")
	pattern := filepath.Join(l.logDir, fmt.Sprintf("decision_%s_*.json", dateStr))

	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("查找日志文件失败: %w", err)
	}

	var records []*DecisionRecord
	for _, filepath := range files {
		data, err := ioutil.ReadFile(filepath)
		if err != nil {
			continue
		}

		var record DecisionRecord
		if err := json.Unmarshal(data, &record); err != nil {
			continue
		}

		records = append(records, &record)
	}

	return records, nil
}

// CleanOldRecords 清理N天前的旧记录
func (l *DecisionLogger) CleanOldRecords(days int) error {
	cutoffTime := time.Now().AddDate(0, 0, -days)

	files, err := ioutil.ReadDir(l.logDir)
	if err != nil {
		return fmt.Errorf("读取日志目录失败: %w", err)
	}

	removedCount := 0
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		if file.ModTime().Before(cutoffTime) {
			filepath := filepath.Join(l.logDir, file.Name())
			if err := os.Remove(filepath); err != nil {
				fmt.Printf("⚠ 删除旧记录失败 %s: %v\n", file.Name(), err)
				continue
			}
			removedCount++
		}
	}

	if removedCount > 0 {
		fmt.Printf("🗑️ 已清理 %d 条旧记录（%d天前）\n", removedCount, days)
	}

	return nil
}

// GetStatistics 获取统计信息
func (l *DecisionLogger) GetStatistics() (*Statistics, error) {
	files, err := ioutil.ReadDir(l.logDir)
	if err != nil {
		return nil, fmt.Errorf("读取日志目录失败: %w", err)
	}

	stats := &Statistics{}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		filepath := filepath.Join(l.logDir, file.Name())
		data, err := ioutil.ReadFile(filepath)
		if err != nil {
			continue
		}

		var record DecisionRecord
		if err := json.Unmarshal(data, &record); err != nil {
			continue
		}

		stats.TotalCycles++

		for _, action := range record.Decisions {
			if action.Success {
				switch action.Action {
				case "open_long", "open_short":
					stats.TotalOpenPositions++
				case "close_long", "close_short":
					stats.TotalClosePositions++
				}
			}
		}

		if record.Success {
			stats.SuccessfulCycles++
		} else {
			stats.FailedCycles++
		}
	}

	return stats, nil
}

// Statistics 统计信息
type Statistics struct {
	TotalCycles         int `json:"total_cycles"`
	SuccessfulCycles    int `json:"successful_cycles"`
	FailedCycles        int `json:"failed_cycles"`
	TotalOpenPositions  int `json:"total_open_positions"`
	TotalClosePositions int `json:"total_close_positions"`
}

// TradeOutcome 单笔交易结果
type TradeOutcome struct {
	Symbol        string    `json:"symbol"`         // 币种
	Side          string    `json:"side"`           // long/short
	Quantity      float64   `json:"quantity"`       // 仓位数量
	Leverage      int       `json:"leverage"`       // 杠杆倍数
	OpenPrice     float64   `json:"open_price"`     // 开仓价
	ClosePrice    float64   `json:"close_price"`    // 平仓价
	PositionValue float64   `json:"position_value"` // 仓位价值（quantity × openPrice）
	MarginUsed    float64   `json:"margin_used"`    // 保证金使用（positionValue / leverage）
	PnL           float64   `json:"pn_l"`           // 盈亏（USDT）
	PnLPct        float64   `json:"pn_l_pct"`       // 盈亏百分比（相对保证金）
	Duration      string    `json:"duration"`       // 持仓时长
	OpenTime      time.Time `json:"open_time"`      // 开仓时间
	CloseTime     time.Time `json:"close_time"`     // 平仓时间
	CloseReason   string    `json:"close_reason"`   // 平仓原因 (e.g., "TP", "SL", "Strategy")
	EntryVWAP     float64   `json:"entry_vwap"`     // 入场时VWAP
	EntryRSI      float64   `json:"entry_rsi"`      // 入场时RSI
	EntryMACD     float64   `json:"entry_macd"`     // 入场时MACD
}

// PerformanceAnalysis 交易表现分析
type PerformanceAnalysis struct {
	TotalTrades   int                           `json:"total_trades"`   // 总交易数
	WinningTrades int                           `json:"winning_trades"` // 盈利交易数
	LosingTrades  int                           `json:"losing_trades"`  // 亏损交易数
	WinRate       float64                       `json:"win_rate"`       // 胜率
	AvgWin        float64                       `json:"avg_win"`        // 平均盈利
	AvgLoss       float64                       `json:"avg_loss"`       // 平均亏损
	ProfitFactor  float64                       `json:"profit_factor"`  // 盈亏比
	SharpeRatio   float64                       `json:"sharpe_ratio"`   // 夏普比率（风险调整后收益）
	RecentTrades  []TradeOutcome                `json:"recent_trades"`  // 最近N笔交易
	SymbolStats   map[string]*SymbolPerformance `json:"symbol_stats"`   // 各币种表现
	BestSymbol    string                        `json:"best_symbol"`    // 表现最好的币种
	WorstSymbol   string                        `json:"worst_symbol"`   // 表现最差的币种
}

// SymbolPerformance 币种表现统计
type SymbolPerformance struct {
	Symbol        string  `json:"symbol"`         // 币种
	TotalTrades   int     `json:"total_trades"`   // 交易次数
	WinningTrades int     `json:"winning_trades"` // 盈利次数
	LosingTrades  int     `json:"losing_trades"`  // 亏损次数
	WinRate       float64 `json:"win_rate"`       // 胜率
	TotalPnL      float64 `json:"total_pn_l"`     // 总盈亏
	AvgPnL        float64 `json:"avg_pn_l"`       // 平均盈亏
}

// AnalyzePerformance 分析最近N个周期的交易表现
func (l *DecisionLogger) AnalyzePerformance(lookbackCycles int) (*PerformanceAnalysis, error) {
	// 扩大窗口以捕获更早的开仓记录，确保平仓能找到对应的开仓
	records, err := l.GetLatestRecords(lookbackCycles * 5)
	if err != nil {
		return nil, fmt.Errorf("读取历史记录失败: %w", err)
	}

	if len(records) == 0 {
		return &PerformanceAnalysis{
			RecentTrades: []TradeOutcome{},
			SymbolStats:  make(map[string]*SymbolPerformance),
		}, nil
	}

	// aiDecision 是 decision.Decision 的本地副本，以避免循环依赖
	type aiDecision struct {
		Symbol     string  `json:"symbol"`
		Action     string  `json:"action"`
		StopLoss   float64 `json:"stop_loss,omitempty"`
		TakeProfit float64 `json:"take_profit,omitempty"`
	}

	type openPositionInfo struct {
		OpenTime   time.Time
		OpenPrice  float64
		Quantity   float64
		Leverage   int
		Side       string
		StopLoss   float64
		TakeProfit float64
		MarketData MarketDataSnapshot
	}
	// 追踪持仓状态: symbol -> openPositionInfo
	openPositions := make(map[string]openPositionInfo)
	
	analysis := &PerformanceAnalysis{
		RecentTrades: []TradeOutcome{},
		SymbolStats:  make(map[string]*SymbolPerformance),
	}

	// 按时间顺序从旧到新遍历所有记录
	for _, record := range records {
		// 1. 解析当前记录中的AI决策，以获取SL/TP
		var decisions []aiDecision
		_ = json.Unmarshal([]byte(record.DecisionJSON), &decisions)
		decisionMap := make(map[string]aiDecision)
		for _, d := range decisions {
			key := d.Symbol + "_" + getSideFromAction(d.Action)
			decisionMap[key] = d
		}

		// 2. 遍历该记录中实际执行的动作
		for _, action := range record.Decisions {
			if !action.Success {
				continue
			}

			side := getSideFromAction(action.Action)
			if side == "" {
				continue
			}
			posKey := action.Symbol

			switch getActionType(action.Action) {
			case "open":
				// 查找对应的AI决策以获取SL/TP
				decisionKey := action.Symbol + "_" + side
				aiDecision, ok := decisionMap[decisionKey]
				sl, tp := 0.0, 0.0
				if ok {
					sl = aiDecision.StopLoss
					tp = aiDecision.TakeProfit
				}

				openPositions[posKey] = openPositionInfo{
					OpenTime:   action.Timestamp,
					OpenPrice:  action.Price,
					Quantity:   action.Quantity,
					Leverage:   action.Leverage,
					Side:       side,
					StopLoss:   sl,
					TakeProfit: tp,
					MarketData: record.MarketData[action.Symbol],
				}

			case "close":
				if openPos, exists := openPositions[posKey]; exists {
					// 确保平仓的边与开仓一致
					if openPos.Side != side {
						continue
					}

					// --- 计算交易结果 ---
					var pnl float64
					if side == "long" {
						pnl = openPos.Quantity * (action.Price - openPos.OpenPrice)
					} else {
						pnl = openPos.Quantity * (openPos.OpenPrice - action.Price)
					}

					positionValue := openPos.Quantity * openPos.OpenPrice
					marginUsed := 0.0
					if openPos.Leverage > 0 {
						marginUsed = positionValue / float64(openPos.Leverage)
					}
					
					pnlPct := 0.0
					if marginUsed > 0 {
						pnlPct = (pnl / marginUsed) * 100
					}

					// --- 判断平仓原因 ---
					closeReason := "Strategy"
					// 允许0.1%的滑点容差
					if side == "long" {
						if openPos.TakeProfit > 0 && action.Price >= openPos.TakeProfit*0.999 {
							closeReason = "TP"
						} else if openPos.StopLoss > 0 && action.Price <= openPos.StopLoss*1.001 {
							closeReason = "SL"
						}
					} else if side == "short" {
						if openPos.TakeProfit > 0 && action.Price <= openPos.TakeProfit*1.001 {
							closeReason = "TP"
						} else if openPos.StopLoss > 0 && action.Price >= openPos.StopLoss*0.999 {
							closeReason = "SL"
						}
					}

					outcome := TradeOutcome{
						Symbol:        action.Symbol,
						Side:          side,
						Quantity:      openPos.Quantity,
						Leverage:      openPos.Leverage,
						OpenPrice:     openPos.OpenPrice,
						ClosePrice:    action.Price,
						PositionValue: positionValue,
						MarginUsed:    marginUsed,
						PnL:           pnl,
						PnLPct:        pnlPct,
						Duration:      action.Timestamp.Sub(openPos.OpenTime).Round(time.Second).String(),
						OpenTime:      openPos.OpenTime,
						CloseTime:     action.Timestamp,
						CloseReason:   closeReason,
						EntryVWAP:     openPos.MarketData.CurrentVWAP,
						EntryRSI:      openPos.MarketData.CurrentRSI7,
						EntryMACD:     openPos.MarketData.CurrentMACD,
					}

					analysis.RecentTrades = append(analysis.RecentTrades, outcome)
					
					// --- 更新统计数据 ---
					analysis.TotalTrades++
					if pnl > 0 {
						analysis.WinningTrades++
						analysis.AvgWin += pnl
					} else if pnl < 0 {
						analysis.LosingTrades++
						analysis.AvgLoss += pnl
					}

					if _, ok := analysis.SymbolStats[action.Symbol]; !ok {
						analysis.SymbolStats[action.Symbol] = &SymbolPerformance{Symbol: action.Symbol}
					}
					stats := analysis.SymbolStats[action.Symbol]
					stats.TotalTrades++
					stats.TotalPnL += pnl
					if pnl > 0 {
						stats.WinningTrades++
					} else if pnl < 0 {
						stats.LosingTrades++
					}

					// 交易完成，从未平仓map中删除
					delete(openPositions, posKey)
				}
			}
		}
	}

	// --- Finalize aggregate statistics ---
	if analysis.TotalTrades > 0 {
		analysis.WinRate = (float64(analysis.WinningTrades) / float64(analysis.TotalTrades)) * 100
		totalWinAmount := analysis.AvgWin
		totalLossAmount := analysis.AvgLoss // This is a negative value
		if analysis.WinningTrades > 0 {
			analysis.AvgWin /= float64(analysis.WinningTrades)
		}
		if analysis.LosingTrades > 0 {
			analysis.AvgLoss /= float64(analysis.LosingTrades)
		}
		if totalLossAmount != 0 {
			analysis.ProfitFactor = totalWinAmount / math.Abs(totalLossAmount)
		} else if totalWinAmount > 0 {
			analysis.ProfitFactor = 999.0 // Infinite profit factor
		}
	}

	bestPnL := -1e9
	worstPnL := 1e9
	for symbol, stats := range analysis.SymbolStats {
		if stats.TotalTrades > 0 {
			stats.WinRate = (float64(stats.WinningTrades) / float64(stats.TotalTrades)) * 100
			stats.AvgPnL = stats.TotalPnL / float64(stats.TotalTrades)
			if stats.TotalPnL > bestPnL {
				bestPnL = stats.TotalPnL
				analysis.BestSymbol = symbol
			}
			if stats.TotalPnL < worstPnL {
				worstPnL = stats.TotalPnL
				analysis.WorstSymbol = symbol
			}
		}
	}

	// 反转，让最新的交易在前
	if len(analysis.RecentTrades) > 0 {
		for i, j := 0, len(analysis.RecentTrades)-1; i < j; i, j = i+1, j-1 {
			analysis.RecentTrades[i], analysis.RecentTrades[j] = analysis.RecentTrades[j], analysis.RecentTrades[i]
		}
	}
	
	// 只保留请求数量的最近交易
	if len(analysis.RecentTrades) > lookbackCycles {
		analysis.RecentTrades = analysis.RecentTrades[:lookbackCycles]
	}

	analysis.SharpeRatio = l.calculateSharpeRatio(records)

	return analysis, nil
}

// --- Helper functions for AnalyzePerformance ---
func getSideFromAction(action string) string {
	if action == "open_long" || action == "close_long" {
		return "long"
	} else if action == "open_short" || action == "close_short" {
		return "short"
	}
	return ""
}

func getActionType(action string) string {
	if action == "open_long" || action == "open_short" {
		return "open"
	} else if action == "close_long" || action == "close_short" {
		return "close"
	}
	return ""
}

// calculateSharpeRatio 计算夏普比率
// 基于账户净值的变化计算风险调整后收益
func (l *DecisionLogger) calculateSharpeRatio(records []*DecisionRecord) float64 {
	if len(records) < 2 {
		return 0.0
	}

	// 提取每个周期的账户净值
	// 注意：TotalBalance字段实际存储的是TotalEquity（账户总净值）
	// TotalUnrealizedProfit字段实际存储的是TotalPnL（相对初始余额的盈亏）
	var equities []float64
	for _, record := range records {
		// 直接使用TotalBalance，因为它已经是完整的账户净值
		equity := record.AccountState.TotalBalance
		if equity > 0 {
			equities = append(equities, equity)
		}
	}

	if len(equities) < 2 {
		return 0.0
	}

	// 计算周期收益率（period returns）
	var returns []float64
	for i := 1; i < len(equities); i++ {
		if equities[i-1] > 0 {
			periodReturn := (equities[i] - equities[i-1]) / equities[i-1]
			returns = append(returns, periodReturn)
		}
	}

	if len(returns) == 0 {
		return 0.0
	}

	// 计算平均收益率
	sumReturns := 0.0
	for _, r := range returns {
		sumReturns += r
	}
	meanReturn := sumReturns / float64(len(returns))

	// 计算收益率标准差
	sumSquaredDiff := 0.0
	for _, r := range returns {
		diff := r - meanReturn
		sumSquaredDiff += diff * diff
	}
	variance := sumSquaredDiff / float64(len(returns))
	stdDev := math.Sqrt(variance)

	// 避免除以零
	if stdDev == 0 {
		if meanReturn > 0 {
			return 999.0 // 无波动的正收益
		} else if meanReturn < 0 {
			return -999.0 // 无波动的负收益
		}
		return 0.0
	}

	// 计算夏普比率（假设无风险利率为0）
	// 注：直接返回周期级别的夏普比率（非年化），正常范围 -2 到 +2
	sharpeRatio := meanReturn / stdDev
	return sharpeRatio
}

// GenerateTradingInsights 生成交易洞察
func GenerateTradingInsights(analysis *PerformanceAnalysis) string {
	if analysis == nil || len(analysis.RecentTrades) == 0 {
		return "没有足够的历史交易来进行复盘。"
	}

	var insights []string

	// 分析最近的5笔交易
	numTradesToAnalyze := 5
	if len(analysis.RecentTrades) < numTradesToAnalyze {
		numTradesToAnalyze = len(analysis.RecentTrades)
	}

	recentTrades := analysis.RecentTrades[:numTradesToAnalyze]

	for _, trade := range recentTrades {
		// 分析亏损交易
		if trade.PnL < 0 {
			// 1. 分析止损交易
			if trade.CloseReason == "SL" {
				insight := fmt.Sprintf("复盘亏损交易[%s %s]: 这是一笔止损(SL)平仓的交易。需要评估入场点和止损位置是否合理。", trade.Symbol, trade.Side)
				insights = append(insights, insight)
			}

			// 2. 分析RSI指标
			if trade.Side == "long" && trade.EntryRSI > 70 {
				insight := fmt.Sprintf("复盘亏损交易[%s %s]: 开多仓时RSI为 %.f，可能处于超买区，有追高风险。建议: 避免在RSI > 70时开多仓。", trade.Symbol, trade.Side, trade.EntryRSI)
				insights = append(insights, insight)
			} else if trade.Side == "short" && trade.EntryRSI < 30 {
				insight := fmt.Sprintf("复盘亏损交易[%s %s]: 开空仓时RSI为 %.f，可能处于超卖区，有杀跌风险。建议: 避免在RSI < 30时开空仓。", trade.Symbol, trade.Side, trade.EntryRSI)
				insights = append(insights, insight)
			}

			// 3. 分析与VWAP的关系
			if trade.Side == "long" && trade.OpenPrice < trade.EntryVWAP {
				insight := fmt.Sprintf("复盘亏损交易[%s %s]: 开多仓时价格低于VWAP，属于逆势交易。建议: 严格遵守价格在VWAP之上时才做多。", trade.Symbol, trade.Side)
				insights = append(insights, insight)
			} else if trade.Side == "short" && trade.OpenPrice > trade.EntryVWAP {
				insight := fmt.Sprintf("复盘亏损交易[%s %s]: 开空仓时价格高于VWAP，属于逆势交易。建议: 严格遵守价格在VWAP之下时才做空。", trade.Symbol, trade.Side)
				insights = append(insights, insight)
			}
		}

		// 分析盈利交易
		if trade.PnL > 0 {
			// 1. 分析成功的VWAP顺势交易
			if trade.Side == "long" && trade.OpenPrice > trade.EntryVWAP {
				insight := fmt.Sprintf("复盘盈利交易[%s %s]: 价格在VWAP之上开多仓，是一次成功的顺势交易。启示: 坚持VWAP顺势交易原则。", trade.Symbol, trade.Side)
				insights = append(insights, insight)
			} else if trade.Side == "short" && trade.OpenPrice < trade.EntryVWAP {
				insight := fmt.Sprintf("复盘盈利交易[%s %s]: 价格在VWAP之下开空仓，是一次成功的顺势交易。启示: 坚持VWAP顺势交易原则。", trade.Symbol, trade.Side)
				insights = append(insights, insight)
			}
		}
	}

	if len(insights) == 0 {
		return "最近的交易没有明显的、可供总结的规律。请继续观察。"
	}

	// 将洞察合并为一段文本
	return "\n# 📈 复盘纪要与进化建议\n" + strings.Join(insights, "\n")
}
