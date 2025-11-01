package decision

import (
	"encoding/json"
	"fmt"
	"log"
	"nofx/market"
	"nofx/mcp"
	"nofx/pool"
	"strings"
	"time"
)

// PositionInfo 持仓信息
type PositionInfo struct {
	Symbol           string  `json:"symbol"`
	Side             string  `json:"side"` // "long" or "short"
	EntryPrice       float64 `json:"entry_price"`
	MarkPrice        float64 `json:"mark_price"`
	Quantity         float64 `json:"quantity"`
	Leverage         int     `json:"leverage"`
	UnrealizedPnL    float64 `json:"unrealized_pnl"`
	UnrealizedPnLPct float64 `json:"unrealized_pnl_pct"`
	LiquidationPrice float64 `json:"liquidation_price"`
	MarginUsed       float64 `json:"margin_used"`
	UpdateTime       int64   `json:"update_time"` // 持仓更新时间戳（毫秒）
}

// AccountInfo 账户信息
type AccountInfo struct {
	TotalEquity      float64 `json:"total_equity"`      // 账户净值
	AvailableBalance float64 `json:"available_balance"` // 可用余额
	TotalPnL         float64 `json:"total_pnl"`         // 总盈亏
	TotalPnLPct      float64 `json:"total_pnl_pct"`     // 总盈亏百分比
	MarginUsed       float64 `json:"margin_used"`       // 已用保证金
	MarginUsedPct    float64 `json:"margin_used_pct"`   // 保证金使用率
	PositionCount    int     `json:"position_count"`    // 持仓数量
}

// CandidateCoin 候选币种（来自币种池）
type CandidateCoin struct {
	Symbol  string   `json:"symbol"`
	Sources []string `json:"sources"` // 来源: "ai500" 和/或 "oi_top"
}

// OITopData 持仓量增长Top数据（用于AI决策参考）
type OITopData struct {
	Rank              int     // OI Top排名
	OIDeltaPercent    float64 // 持仓量变化百分比（1小时）
	OIDeltaValue      float64 // 持仓量变化价值
	PriceDeltaPercent float64 // 价格变化百分比
	NetLong           float64 // 净多仓
	NetShort          float64 // 净空仓
}

// Context 交易上下文（传递给AI的完整信息）
type Context struct {
	CurrentTime     string                  `json:"current_time"`
	RuntimeMinutes  int                     `json:"runtime_minutes"`
	CallCount       int                     `json:"call_count"`
	Account         AccountInfo             `json:"account"`
	Positions       []PositionInfo          `json:"positions"`
	CandidateCoins  []CandidateCoin         `json:"candidate_coins"`
	MarketDataMap   map[string]*market.Data `json:"-"` // 不序列化，但内部使用
	OITopDataMap    map[string]*OITopData   `json:"-"` // OI Top数据映射
	Performance     interface{}             `json:"-"` // 历史表现分析（logger.PerformanceAnalysis）
	BTCETHLeverage  int                     `json:"-"` // BTC/ETH杠杆倍数（从配置读取）
	AltcoinLeverage int                     `json:"-"` // 山寨币杠杆倍数（从配置读取）
	TradingInsights string                  `json:"-"` // 交易复盘洞察
}

// Decision AI的交易决策
type Decision struct {
	Symbol          string  `json:"symbol"`
	Action          string  `json:"action"` // "open_long", "open_short", "close_long", "close_short", "hold", "wait"
	Leverage        int     `json:"leverage,omitempty"`
	PositionSizeUSD float64 `json:"position_size_usd,omitempty"`
	StopLoss        float64 `json:"stop_loss,omitempty"`
	TakeProfit      float64 `json:"take_profit,omitempty"`
	Confidence      int     `json:"confidence,omitempty"` // 信心度 (0-100)
	RiskUSD         float64 `json:"risk_usd,omitempty"`   // 最大美元风险
	Reasoning       string  `json:"reasoning"`
}

// FullDecision AI的完整决策（包含思维链）
type FullDecision struct {
	UserPrompt string     `json:"user_prompt"` // 发送给AI的输入prompt
	CoTTrace   string     `json:"cot_trace"`   // 思维链分析（AI输出）
	Decisions       []Decision `json:"decisions"`   // 具体决策列表
	ValidationTrace []string   `json:"validation_trace"` // 交叉验证记录
	Timestamp       time.Time  `json:"timestamp"`
}

// GetFullDecision 获取AI的完整交易决策（包含双模型交叉验证）
func GetFullDecision(ctx *Context, primaryClient *mcp.Client, secondaryClient *mcp.Client) (*FullDecision, error) {
	// 1. 为所有币种获取市场数据
	if err := fetchMarketDataForContext(ctx); err != nil {
		return nil, fmt.Errorf("获取市场数据失败: %w", err)
	}

	// 2. 构建 Prompt
	systemPrompt := buildSystemPrompt(ctx.Account.TotalEquity, ctx.BTCETHLeverage, ctx.AltcoinLeverage)
	userPrompt := buildUserPrompt(ctx)

	// 3. 调用主模型(DeepSeek)获取初步决策
	primaryResponse, err := primaryClient.CallWithMessages(systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("调用主模型AI API失败: %w", err)
	}

	// 4. 解析主模型响应
	primaryDecision, err := parseFullDecisionResponse(primaryResponse, ctx, ctx.Account.TotalEquity, ctx.BTCETHLeverage, ctx.AltcoinLeverage)
	if err != nil {
		// 即使解析失败，也返回思维链，方便调试
		if primaryDecision != nil {
			primaryDecision.UserPrompt = userPrompt
		}
		return primaryDecision, fmt.Errorf("解析主模型响应失败: %w", err)
	}
	primaryDecision.UserPrompt = userPrompt

	// 5. 执行交叉验证 (只对开仓决策)
	var finalDecisions []Decision
	var validationTrace []string

	log.Println("🤖 正在请求验证模型(Qwen)进行交叉验证...")

	for _, decision := range primaryDecision.Decisions {
		// 只对开仓决策进行二次验证
		if decision.Action == "open_long" || decision.Action == "open_short" {
			// 为验证模型构建专用prompt
			validationPrompt := buildValidationPrompt(ctx, &decision)

			// 调用验证模型
			validationResponse, err := secondaryClient.CallWithMessages("", validationPrompt) // System prompt is empty for validation
			if err != nil {
				// 如果验证模型调用失败，为安全起见，拒绝该决策
				trace := fmt.Sprintf("- 验证 %s %s: 失败 (API错误: %v)。决策被拒绝。", decision.Symbol, decision.Action, err)
				validationTrace = append(validationTrace, trace)
				log.Println(trace)
				continue
			}

			// 检查验证模型的响应
			if strings.Contains(strings.ToUpper(validationResponse), "AGREE") {
				// 验证通过
				trace := fmt.Sprintf("- 验证 %s %s: 通过 (AGREE)", decision.Symbol, decision.Action)
				validationTrace = append(validationTrace, trace)
				log.Println(trace)

				// 在Reasoning中加入验证信息
				decision.Reasoning += " (Qwen验证通过)"
				finalDecisions = append(finalDecisions, decision)
			} else {
				// 验证拒绝
				trace := fmt.Sprintf("- 验证 %s %s: 拒绝 (DISAGREE)。原始原因: %s", decision.Symbol, decision.Action, decision.Reasoning)
				validationTrace = append(validationTrace, trace)
				log.Println(trace)
			}
		} else {
			// 对于非开仓决策 (close, hold, wait)，直接采纳
			finalDecisions = append(finalDecisions, decision)
		}
	}

	primaryDecision.Decisions = finalDecisions
	primaryDecision.ValidationTrace = validationTrace
	primaryDecision.Timestamp = time.Now()

	return primaryDecision, nil
}

// buildValidationPrompt 为验证模型构建专用的prompt
func buildValidationPrompt(ctx *Context, decision *Decision) string {
	var sb strings.Builder
	sb.WriteString("你是一个严谨的交易策略验证助手。请根据提供的VWAP策略规则和市场数据，判断以下交易决策是否合理。")
	sb.WriteString("请只回答 'AGREE' 或 'DISAGREE'。\n\n")
	sb.WriteString("# VWAP策略核心规则\n")
	sb.WriteString("- 做多信号: `价格 > VWAP`，且 `RSI < 70`，`MACD > 0`。\n")
	sb.WriteString("- 做空信号: `价格 < VWAP`，且 `RSI > 30`，`MACD < 0`。\n\n")

	sb.WriteString("# 待验证决策\n")
	sb.WriteString(fmt.Sprintf("- 币种: %s\n", decision.Symbol))
	sb.WriteString(fmt.Sprintf("- 方向: %s\n", decision.Action))
	sb.WriteString(fmt.Sprintf("- 理由: %s\n\n", decision.Reasoning))

	sb.WriteString("# 相关市场数据\n")
	if marketData, ok := ctx.MarketDataMap[decision.Symbol]; ok {
		sb.WriteString(market.Format(marketData))
	} else {
		sb.WriteString("未找到该币种的市场数据。\n")
	}

	sb.WriteString("\n请判断此决策是否符合VWAP策略规则？请只回答 'AGREE' 或 'DISAGREE'。")

	return sb.String()
}

// fetchMarketDataForContext 为上下文中的所有币种获取市场数据和OI数据
func fetchMarketDataForContext(ctx *Context) error {
	ctx.MarketDataMap = make(map[string]*market.Data)
	ctx.OITopDataMap = make(map[string]*OITopData)

	// 收集所有需要获取数据的币种
	symbolSet := make(map[string]bool)

	// 1. 优先获取持仓币种的数据（这是必须的）
	for _, pos := range ctx.Positions {
		symbolSet[pos.Symbol] = true
	}

	// 2. 候选币种数量根据账户状态动态调整
	maxCandidates := calculateMaxCandidates(ctx)
	for i, coin := range ctx.CandidateCoins {
		if i >= maxCandidates {
			break
		}
		symbolSet[coin.Symbol] = true
	}

	// 并发获取市场数据
	// 持仓币种集合（用于判断是否跳过OI检查）
	positionSymbols := make(map[string]bool)
	for _, pos := range ctx.Positions {
		positionSymbols[pos.Symbol] = true
	}

	for symbol := range symbolSet {
		data, err := market.Get(symbol)
		if err != nil {
			// 单个币种失败不影响整体，只记录错误
			continue
		}

		// ⚠️ 流动性过滤：持仓价值低于15M USD的币种不做（多空都不做）
		// 持仓价值 = 持仓量 × 当前价格
		// 但现有持仓必须保留（需要决策是否平仓）
		isExistingPosition := positionSymbols[symbol]
		if !isExistingPosition && data.OpenInterest != nil && data.CurrentPrice > 0 {
			// 计算持仓价值（USD）= 持仓量 × 当前价格
			oiValue := data.OpenInterest.Latest * data.CurrentPrice
			oiValueInMillions := oiValue / 1_000_000 // 转换为百万美元单位
			if oiValueInMillions < 15 {
				log.Printf("⚠️  %s 持仓价值过低(%.2fM USD < 15M)，跳过此币种 [持仓量:%.0f × 价格:%.4f]",
					symbol, oiValueInMillions, data.OpenInterest.Latest, data.CurrentPrice)
				continue
			}
		}

		ctx.MarketDataMap[symbol] = data
	}

	// 加载OI Top数据（不影响主流程）
	oiPositions, err := pool.GetOITopPositions()
	if err == nil {
		for _, pos := range oiPositions {
			// 标准化符号匹配
			symbol := pos.Symbol
			ctx.OITopDataMap[symbol] = &OITopData{
				Rank:              pos.Rank,
				OIDeltaPercent:    pos.OIDeltaPercent,
				OIDeltaValue:      pos.OIDeltaValue,
				PriceDeltaPercent: pos.PriceDeltaPercent,
				NetLong:           pos.NetLong,
				NetShort:          pos.NetShort,
			}
		}
	}

	return nil
}

// calculateMaxCandidates 根据账户状态计算需要分析的候选币种数量
func calculateMaxCandidates(ctx *Context) int {
	// 直接返回候选池的全部币种数量
	// 因为候选池已经在 auto_trader.go 中筛选过了
	// 固定分析前20个评分最高的币种（来自AI500）
	return len(ctx.CandidateCoins)
}

// buildSystemPrompt 构建 System Prompt（固定规则，可缓存）
func buildSystemPrompt(accountEquity float64, btcEthLeverage, altcoinLeverage int) string {
	var sb strings.Builder

	// === 核心策略：VWAP 趋势跟踪 ===
	sb.WriteString("你是专业的加密货币交易AI，负责执行一个基于VWAP的日内交易策略。\n\n")
	sb.WriteString("# 🎯 核心目标\n")
	sb.WriteString("严格遵循VWAP交易规则，结合RSI和MACD进行确认，找到高胜率的交易机会。\n\n")

	sb.WriteString("# ⚖️ 交易规则 (VWAP策略)\n\n")
	sb.WriteString("## 做多 (Long) 信号:\n")
	sb.WriteString("1. **主要条件**: `current_price` (当前价格) > `current_vwap` (VWAP值)。价格在VWAP之上，表明处于日内强势区域。\n")
	sb.WriteString("2. **入场时机**: 寻找价格从下方上穿VWAP，或者回踩VWAP并获得支撑后再次上涨的时刻。\n")
	sb.WriteString("3. **确认指标**: \n")
	sb.WriteString("   - `current_rsi` (RSI) < 70 (避免在超买区追高)。\n")
	sb.WriteString("   - `current_macd` (MACD) > 0 或正在上行 (趋势确认)。\n")
	sb.WriteString("4. **综合信心度**: 只有当主要条件和确认指标都满足时，才认为是高信心度机会 (confidence >= 75)。\n\n")

	sb.WriteString("## 做空 (Short) 信号:\n")
	sb.WriteString("1. **主要条件**: `current_price` (当前价格) < `current_vwap` (VWAP值)。价格在VWAP之下，表明处于日内弱势区域。\n")
	sb.WriteString("2. **入场时机**: 寻找价格从上方下穿VWAP，或者反弹至VWAP并受阻后再次下跌的时刻。\n")
	sb.WriteString("3. **确认指标**: \n")
	sb.WriteString("   - `current_rsi` (RSI) > 30 (避免在超卖区杀跌)。\n")
	sb.WriteString("   - `current_macd` (MACD) < 0 或正在下行 (趋势确认)。\n")
	sb.WriteString("4. **综合信心度**: 只有当主要条件和确认指标都满足时，才认为是高信心度机会 (confidence >= 75)。\n\n")

	sb.WriteString("## 平仓/持仓 规则:\n")
	sb.WriteString("- **持有多单 (hold long)**: 只要 `current_price` > `current_vwap`，就继续持有多单。\n")
	sb.WriteString("- **持有空单 (hold short)**: 只要 `current_price` < `current_vwap`，就继续持有空单。\n")
	sb.WriteString("- **平仓信号**: 当价格反向穿越VWAP时，应考虑平仓。例如，持有多单时，价格下穿VWAP，则平仓。\n\n")

	// === 风险控制 ===
	sb.WriteString("# 🛡️ 风险控制 (硬约束)\n\n")
	sb.WriteString("1. **风险回报比**: 必须 ≥ 1:2。例如，如果止损设置为亏损1%，止盈至少要达到2%。\n")
	sb.WriteString("2. **止损 (Stop-Loss)**: \n")
	sb.WriteString("   - **做多时**: 止损价应设置在VWAP价格下方的一个合理位置。\n")
	sb.WriteString("   - **做空时**: 止损价应设置在VWAP价格上方的一个合理位置。\n")
	sb.WriteString("3. **最多持仓**: 最多同时持有 3 个币种。\n")
	sb.WriteString(fmt.Sprintf("4. **单币仓位**: 山寨币 %.0f-%.0f U, BTC/ETH %.0f-%.0f U。\n",
		accountEquity*0.8, accountEquity*1.5, accountEquity*5, accountEquity*10))
	sb.WriteString(fmt.Sprintf("5. **杠杆**: 山寨币不超过 %dx, BTC/ETH 不超过 %dx。\n\n", altcoinLeverage, btcEthLeverage))

	// === 决策流程 ===
	sb.WriteString("# 🧠 自我反思与进化\n\n")
	sb.WriteString("**决策前必须先复盘！**\n")
	sb.WriteString("在你的决策流程第一步，你将收到一份`复盘纪要与进化建议`。\n")
	sb.WriteString("这份纪要分析了最近的交易，指出了亏损的原因和盈利的模式。\n\n")
	sb.WriteString("**你的任务**: \n")
	sb.WriteString("1. **深刻理解**纪要中的每一条建议和启示。\n")
	sb.WriteString("2. **严格执行**纪要中的建议。例如，如果建议`避免在RSI > 70时开多仓`，你在本次决策中就必须遵守。\n")
	sb.WriteString("3. 在你的思维链分析中，**明确回应**你将如何根据这些建议调整你的本次决策。\n\n")
	sb.WriteString("这是你实现自我进化的核心，必须严格执行。\n\n")

	sb.WriteString("# 📋 决策流程\n\n")
	sb.WriteString("1. **分析持仓**: 根据VWAP规则，判断现有持仓是应该 `hold` 还是 `close`。\n")
	sb.WriteString("2. **寻找新机会**: 遍历候选币种，寻找满足VWAP做多或做空信号的币种。\n")
	sb.WriteString("3. **给出决策**: 如果没有机会，对所有币种使用 `wait`。如果有机会，给出 `open_long` 或 `open_short` 决策，并提供所有必要参数。\n\n")

	// === 输出格式 ===
	sb.WriteString("# 📤 输出格式 (保持不变)\n\n")
	sb.WriteString("保持之前的思维链 + JSON格式。\n")
	sb.WriteString("```json\n[\n")
	sb.WriteString("  {\"symbol\": \"BTCUSDT\", \"action\": \"open_long\", \"leverage\": 10, \"position_size_usd\": 5000, \"stop_loss\": 68000, \"take_profit\": 72000, \"confidence\": 80, \"risk_usd\": 200, \"reasoning\": \"价格上穿VWAP，RSI<70，MACD上行，满足做多条件。\"}\n")
	sb.WriteString("]\n```\n")

	return sb.String()
}

// buildUserPrompt 构建 User Prompt（动态数据）
func buildUserPrompt(ctx *Context) string {
	var sb strings.Builder

	// 系统状态
	sb.WriteString(fmt.Sprintf("**时间**: %s | **周期**: #%d | **运行**: %d分钟\n\n",
		ctx.CurrentTime, ctx.CallCount, ctx.RuntimeMinutes))

	// BTC 市场
	if btcData, hasBTC := ctx.MarketDataMap["BTCUSDT"]; hasBTC {
		sb.WriteString(fmt.Sprintf("**BTC**: %.2f (1h: %+.2f%%, 4h: %+.2f%%) | VWAP: %.2f | MACD: %.4f | RSI: %.2f\n\n",
			btcData.CurrentPrice, btcData.PriceChange1h, btcData.PriceChange4h, btcData.CurrentVWAP,
			btcData.CurrentMACD, btcData.CurrentRSI7))
	}

	// 账户
	sb.WriteString(fmt.Sprintf("**账户**: 净值%.2f | 余额%.2f (%.1f%%) | 盈亏%+.2f%% | 保证金%.1f%% | 持仓%d个\n\n",
		ctx.Account.TotalEquity,
		ctx.Account.AvailableBalance,
		(ctx.Account.AvailableBalance/ctx.Account.TotalEquity)*100,
		ctx.Account.TotalPnLPct,
		ctx.Account.MarginUsedPct,
		ctx.Account.PositionCount))

	// 持仓（完整市场数据）
	if len(ctx.Positions) > 0 {
		sb.WriteString("## 当前持仓\n")
		for i, pos := range ctx.Positions {
			// 计算持仓时长
			holdingDuration := ""
			if pos.UpdateTime > 0 {
				durationMs := time.Now().UnixMilli() - pos.UpdateTime
				durationMin := durationMs / (1000 * 60) // 转换为分钟
				if durationMin < 60 {
					holdingDuration = fmt.Sprintf(" | 持仓时长%d分钟", durationMin)
				} else {
					durationHour := durationMin / 60
					durationMinRemainder := durationMin % 60
					holdingDuration = fmt.Sprintf(" | 持仓时长%d小时%d分钟", durationHour, durationMinRemainder)
				}
			}

			sb.WriteString(fmt.Sprintf("%d. %s %s | 入场价%.4f 当前价%.4f | 盈亏%+.2f%% | 杠杆%dx | 保证金%.0f | 强平价%.4f%s\n\n",
				i+1, pos.Symbol, strings.ToUpper(pos.Side),
				pos.EntryPrice, pos.MarkPrice, pos.UnrealizedPnLPct,
				pos.Leverage, pos.MarginUsed, pos.LiquidationPrice, holdingDuration))

			// 使用FormatMarketData输出完整市场数据
			if marketData, ok := ctx.MarketDataMap[pos.Symbol]; ok {
				sb.WriteString(market.Format(marketData))
				sb.WriteString("\n")
			}
		}
	} else {
		sb.WriteString("**当前持仓**: 无\n\n")
	}

	// 候选币种（完整市场数据）
	sb.WriteString(fmt.Sprintf("## 候选币种 (%d个)\n\n", len(ctx.MarketDataMap)))
	displayedCount := 0
	for _, coin := range ctx.CandidateCoins {
		marketData, hasData := ctx.MarketDataMap[coin.Symbol]
		if !hasData {
			continue
		}
		displayedCount++

		sourceTags := ""
		if len(coin.Sources) > 1 {
			sourceTags = " (AI500+OI_Top双重信号)"
		} else if len(coin.Sources) == 1 && coin.Sources[0] == "oi_top" {
			sourceTags = " (OI_Top持仓增长)"
		}

		// 使用FormatMarketData输出完整市场数据
		sb.WriteString(fmt.Sprintf("### %d. %s%s\n\n", displayedCount, coin.Symbol, sourceTags))
		sb.WriteString(market.Format(marketData))
		sb.WriteString("\n")
	}
	sb.WriteString("\n")

	// 夏普比率（直接传值，不要复杂格式化）
	if ctx.Performance != nil {
		// 直接从interface{}中提取SharpeRatio
		type PerformanceData struct {
			SharpeRatio float64 `json:"sharpe_ratio"`
		}
		var perfData PerformanceData
		if jsonData, err := json.Marshal(ctx.Performance); err == nil {
			if err := json.Unmarshal(jsonData, &perfData); err == nil {
				sb.WriteString(fmt.Sprintf("## 📊 夏普比率: %.2f\n\n", perfData.SharpeRatio))
			}
		}
	}

	// 交易洞察（复盘纪要）
	if ctx.TradingInsights != "" {
		sb.WriteString(ctx.TradingInsights)
		sb.WriteString("\n")
	}

	sb.WriteString("---\n\n")
	sb.WriteString("现在请分析并输出决策（思维链 + JSON）\n")

	return sb.String()
}

// parseFullDecisionResponse 解析AI的完整决策响应
func parseFullDecisionResponse(aiResponse string, ctx *Context, accountEquity float64, btcEthLeverage, altcoinLeverage int) (*FullDecision, error) {
	// 1. 提取思维链
	cotTrace := extractCoTTrace(aiResponse)

	// 2. 提取JSON决策列表
	decisions, err := extractDecisions(aiResponse)
	if err != nil {
		return &FullDecision{
			CoTTrace:  cotTrace,
			Decisions: []Decision{},
		}, fmt.Errorf("提取决策失败: %w\n\n=== AI思维链分析 ===\n%s", err, cotTrace)
	}

	// 3. 标准化决策 (例如, 'close' -> 'close_long')
	normalizeDecisions(decisions, ctx.Positions)

	// 4. 验证决策
	if err := validateDecisions(decisions, accountEquity, btcEthLeverage, altcoinLeverage); err != nil {
		return &FullDecision{
			CoTTrace:  cotTrace,
			Decisions: decisions,
		}, fmt.Errorf("决策验证失败: %w\n\n=== AI思维链分析 ===\n%s", err, cotTrace)
	}

	return &FullDecision{
		CoTTrace:  cotTrace,
		Decisions: decisions,
	}, nil
}

// extractCoTTrace 提取思维链分析
func extractCoTTrace(response string) string {
	// 查找JSON数组的开始位置
	jsonStart := strings.Index(response, "[")

	if jsonStart > 0 {
		// 思维链是JSON数组之前的内容
		return strings.TrimSpace(response[:jsonStart])
	}

	// 如果找不到JSON，整个响应都是思维链
	return strings.TrimSpace(response)
}

// extractDecisions 提取JSON决策列表
func extractDecisions(response string) ([]Decision, error) {
	// 直接查找JSON数组 - 找第一个完整的JSON数组
	arrayStart := strings.Index(response, "[")
	if arrayStart == -1 {
		return nil, fmt.Errorf("无法找到JSON数组起始")
	}

	// 从 [ 开始，匹配括号找到对应的 ]
	arrayEnd := findMatchingBracket(response, arrayStart)
	if arrayEnd == -1 {
		return nil, fmt.Errorf("无法找到JSON数组结束")
	}

	jsonContent := strings.TrimSpace(response[arrayStart : arrayEnd+1])

	// 🔧 修复常见的JSON格式错误：缺少引号的字段值
	// 匹配: "reasoning": 内容"}  或  "reasoning": 内容}  (没有引号)
	// 修复为: "reasoning": "内容"}
	// 使用简单的字符串扫描而不是正则表达式
	jsonContent = fixMissingQuotes(jsonContent)

	// 解析JSON
	var decisions []Decision
	if err := json.Unmarshal([]byte(jsonContent), &decisions); err != nil {
		return nil, fmt.Errorf("JSON解析失败: %w\nJSON内容: %s", err, jsonContent)
	}

	return decisions, nil
}

// fixMissingQuotes 替换中文引号为英文引号（避免输入法自动转换）
func fixMissingQuotes(jsonStr string) string {
	jsonStr = strings.ReplaceAll(jsonStr, "\u201c", "\"") // "
	jsonStr = strings.ReplaceAll(jsonStr, "\u201d", "\"") // "
	jsonStr = strings.ReplaceAll(jsonStr, "\u2018", "'")  // '
	jsonStr = strings.ReplaceAll(jsonStr, "\u2019", "'")  // '
	return jsonStr
}

// normalizeDecisions 标准化AI决策
// 1. 将 'hold_long'/'hold_short' 统一为 'hold'
// 2. 将 'close' 转换为 'close_long' 或 'close_short'
func normalizeDecisions(decisions []Decision, positions []PositionInfo) {
	// 创建一个map以便快速查找持仓方向
	positionSides := make(map[string]string)
	if positions != nil {
		for _, pos := range positions {
			positionSides[pos.Symbol] = pos.Side
		}
	}

	for i := range decisions {
		d := &decisions[i] // 使用指针直接修改切片中的元素

		// 统一 hold action
		if d.Action == "hold_long" || d.Action == "hold_short" {
			d.Action = "hold"
		}

		// 转换通用的 close action
		if d.Action == "close" {
			if side, ok := positionSides[d.Symbol]; ok {
				d.Action = "close_" + side // "close_long" or "close_short"
			} else {
				// 如果在持仓中找不到该币种，则此close决策无效
				// 可以在验证阶段处理，这里暂时标记为未知
				d.Action = "unknown_close"
			}
		}
	}
}

// validateDecisions 验证所有决策（需要账户信息和杠杆配置）
func validateDecisions(decisions []Decision, accountEquity float64, btcEthLeverage, altcoinLeverage int) error {
	for i, decision := range decisions {
		if err := validateDecision(&decision, accountEquity, btcEthLeverage, altcoinLeverage); err != nil {
			return fmt.Errorf("决策 #%d 验证失败: %w", i+1, err)
		}
	}
	return nil
}

// findMatchingBracket 查找匹配的右括号
func findMatchingBracket(s string, start int) int {
	if start >= len(s) || s[start] != '[' {
		return -1
	}

	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return i
			}
		}
	}

	return -1
}

// validateDecision 验证单个决策的有效性
func validateDecision(d *Decision, accountEquity float64, btcEthLeverage, altcoinLeverage int) error {
	// 验证action
	validActions := map[string]bool{
		"open_long":   true,
		"open_short":  true,
		"close_long":  true,
		"close_short": true,
		"hold":        true,
		"wait":        true,
	}

	if !validActions[d.Action] {
		return fmt.Errorf("无效的action: %s", d.Action)
	}

	// 开仓操作必须提供完整参数
	if d.Action == "open_long" || d.Action == "open_short" {
		// 根据币种使用配置的杠杆上限
		maxLeverage := altcoinLeverage          // 山寨币使用配置的杠杆
		maxPositionValue := accountEquity * 1.5 // 山寨币最多1.5倍账户净值
		if d.Symbol == "BTCUSDT" || d.Symbol == "ETHUSDT" {
			maxLeverage = btcEthLeverage          // BTC和ETH使用配置的杠杆
			maxPositionValue = accountEquity * 10 // BTC/ETH最多10倍账户净值
		}

		if d.Leverage <= 0 || d.Leverage > maxLeverage {
			return fmt.Errorf("杠杆必须在1-%d之间（%s，当前配置上限%d倍）: %d", maxLeverage, d.Symbol, maxLeverage, d.Leverage)
		}
		if d.PositionSizeUSD <= 0 {
			return fmt.Errorf("仓位大小必须大于0: %.2f", d.PositionSizeUSD)
		}
		// 验证仓位价值上限（加1%容差以避免浮点数精度问题）
		tolerance := maxPositionValue * 0.01 // 1%容差
		if d.PositionSizeUSD > maxPositionValue+tolerance {
			if d.Symbol == "BTCUSDT" || d.Symbol == "ETHUSDT" {
				return fmt.Errorf("BTC/ETH单币种仓位价值不能超过%.0f USDT（10倍账户净值），实际: %.0f", maxPositionValue, d.PositionSizeUSD)
			} else {
				return fmt.Errorf("山寨币单币种仓位价值不能超过%.0f USDT（1.5倍账户净值），实际: %.0f", maxPositionValue, d.PositionSizeUSD)
			}
		}
		if d.StopLoss <= 0 || d.TakeProfit <= 0 {
			return fmt.Errorf("止损和止盈必须大于0")
		}

		// 验证止损止盈的合理性
		if d.Action == "open_long" {
			if d.StopLoss >= d.TakeProfit {
				return fmt.Errorf("做多时止损价必须小于止盈价")
			}
		} else {
			if d.StopLoss <= d.TakeProfit {
				return fmt.Errorf("做空时止损价必须大于止盈价")
			}
		}

		// 验证风险回报比（必须≥1:3）
		// 计算入场价（假设当前市价）
		var entryPrice float64
		if d.Action == "open_long" {
			// 做多：入场价在止损和止盈之间
			entryPrice = d.StopLoss + (d.TakeProfit-d.StopLoss)*0.2 // 假设在20%位置入场
		} else {
			// 做空：入场价在止损和止盈之间
			entryPrice = d.StopLoss - (d.StopLoss-d.TakeProfit)*0.2 // 假设在20%位置入场
		}

		var riskPercent, rewardPercent, riskRewardRatio float64
		if d.Action == "open_long" {
			riskPercent = (entryPrice - d.StopLoss) / entryPrice * 100
			rewardPercent = (d.TakeProfit - entryPrice) / entryPrice * 100
			if riskPercent > 0 {
				riskRewardRatio = rewardPercent / riskPercent
			}
		} else {
			riskPercent = (d.StopLoss - entryPrice) / entryPrice * 100
			rewardPercent = (entryPrice - d.TakeProfit) / entryPrice * 100
			if riskPercent > 0 {
				riskRewardRatio = rewardPercent / riskPercent
			}
		}

		// 硬约束：风险回报比必须≥3.0
		if riskRewardRatio < 3.0 {
			return fmt.Errorf("风险回报比过低(%.2f:1)，必须≥3.0:1 [风险:%.2f%% 收益:%.2f%%] [止损:%.2f 止盈:%.2f]",
				riskRewardRatio, riskPercent, rewardPercent, d.StopLoss, d.TakeProfit)
		}
	}

	return nil
}
