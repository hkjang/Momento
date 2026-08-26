// Package insight builds the visitor insight report: comparison-ready KPIs,
// audience structure and ranked findings that state their own evidence. The
// console, the MCP surface and scheduled delivery all read the same report so a
// narrative never depends on which door it came through.
package insight

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Reporter reads the analytics tables. It never writes.
type Reporter struct{ DB *pgxpool.Pool }

func New(db *pgxpool.Pool) Reporter { return Reporter{DB: db} }

// percent and percentChange keep the report self-contained.
func percent(part, total int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) * 100 / float64(total)
}

func percentChange(current, previous float64) float64 {
	if previous == 0 {
		if current == 0 {
			return 0
		}
		return 100
	}
	return (current - previous) * 100 / previous
}

// periodMetrics are the visitor-level aggregates a comparison needs.
type periodMetrics struct {
	Users           int64
	NewUsers        int64
	Sessions        int64
	PageViews       int64
	ConvertedUsers  int64
	EngagedSessions int64
	AverageSeconds  float64
}

func (m periodMetrics) engagementRate() float64 { return percent(m.EngagedSessions, m.Sessions) }
func (m periodMetrics) conversionRate() float64 { return percent(m.ConvertedUsers, m.Users) }
func (m periodMetrics) newUserShare() float64   { return percent(m.NewUsers, m.Users) }
func (m periodMetrics) returningUsers() int64   { return m.Users - m.NewUsers }

// Visitor Insights answers the three questions an analyst asks first: who visited,
// what changed against the previous period, and what to do next. Every number is
// paired with a comparison and a ranked finding so the report can be taken away
// as-is instead of being reassembled from separate screens.

// KPI carries the comparison and how to read it. Goal is "higher", "lower" or
// "neutral", because a rising share of first-time visitors is neither good nor
// bad on its own and must not be coloured as progress.
type KPI struct {
	Key           string  `json:"key"`
	Label         string  `json:"label"`
	Format        string  `json:"format"`
	Current       float64 `json:"current"`
	Previous      float64 `json:"previous"`
	ChangePercent float64 `json:"change_percent"`
	Goal          string  `json:"goal"`
}

type Finding struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Severity string  `json:"severity"`
	Evidence string  `json:"evidence"`
	Cause    string  `json:"cause"`
	Action   string  `json:"action"`
	Impact   float64 `json:"impact"`
}

type ChannelRow struct {
	Channel          string  `json:"channel"`
	Users            int64   `json:"users"`
	Sessions         int64   `json:"sessions"`
	ConvertedUsers   int64   `json:"converted_users"`
	ConversionRate   float64 `json:"conversion_rate"`
	PreviousUsers    int64   `json:"previous_users"`
	ChangePercent    float64 `json:"change_percent"`
	UserSharePercent float64 `json:"user_share_percent"`
}

type LandingRow struct {
	Page           string  `json:"page"`
	Sessions       int64   `json:"sessions"`
	BounceRate     float64 `json:"bounce_rate"`
	EngagementRate float64 `json:"engagement_rate"`
	ConversionRate float64 `json:"conversion_rate"`
	AverageSeconds float64 `json:"average_seconds"`
	SessionShare   float64 `json:"session_share_percent"`
}

type BucketRow struct {
	Bucket         string  `json:"bucket"`
	Label          string  `json:"label"`
	Users          int64   `json:"users"`
	SharePercent   float64 `json:"share_percent"`
	ConversionRate float64 `json:"conversion_rate"`
}

type DeviceRow struct {
	Device         string  `json:"device"`
	Users          int64   `json:"users"`
	Sessions       int64   `json:"sessions"`
	ConversionRate float64 `json:"conversion_rate"`
	SharePercent   float64 `json:"share_percent"`
}

type LifecycleRow struct {
	Kind            string  `json:"kind"`
	Users           int64   `json:"users"`
	Sessions        int64   `json:"sessions"`
	SessionsPerUser float64 `json:"sessions_per_user"`
	ConversionRate  float64 `json:"conversion_rate"`
	SharePercent    float64 `json:"share_percent"`
}

// channelSource is the raw acquisition grain read from the database before it
// is folded into channel groups.
type channelSource struct {
	Source        string
	Medium        string
	HasReferrer   bool
	Internal      bool
	Users         int64
	Sessions      int64
	Converted     int64
	PreviousUsers int64
}

// ClassifyChannel folds a source/medium pair into a channel group. The grouping
// follows the well known web analytics defaults and adds the internal portal and
// notice channels that matter for an on-premise employee analytics deployment.
func ClassifyChannel(source, medium string, hasReferrer, internalNetwork bool) string {
	source = strings.ToLower(strings.TrimSpace(source))
	medium = strings.ToLower(strings.TrimSpace(medium))
	switch {
	case containsAny(medium, "cpc", "ppc", "paid", "cpm", "cpv"):
		return "Paid Search"
	case medium == "organic" || containsAny(source, "google", "naver", "daum", "bing", "yahoo", "duckduckgo", "kagi"):
		return "Organic Search"
	case containsAny(medium, "email", "mail", "newsletter") || containsAny(source, "mailgateway", "outlook", "exchange"):
		return "Email"
	case containsAny(medium, "social") || containsAny(source, "facebook", "instagram", "twitter", "linkedin", "kakao", "band", "youtube", "threads", "x.com"):
		return "Social"
	case containsAny(medium, "notice", "announcement", "공지"):
		return "Internal Notice"
	case containsAny(medium, "internal", "intranet", "portal") || containsAny(source, "intranet", "portal", "groupware", "confluence", "sharepoint"):
		return "Internal Portal"
	case containsAny(medium, "display", "banner", "signage"):
		return "Display"
	case containsAny(medium, "messenger", "chat", "teams", "slack"):
		return "Internal Message"
	case medium == "referral" || hasReferrer:
		return "Referral"
	case source == "" && medium == "":
		if internalNetwork {
			return "Direct (사내망)"
		}
		return "Direct"
	default:
		return "Other"
	}
}

func containsAny(value string, needles ...string) bool {
	if value == "" {
		return false
	}
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

// GroupChannels aggregates source grain rows into ranked channel groups.
func GroupChannels(rows []channelSource, totalUsers int64) []ChannelRow {
	merged := map[string]*ChannelRow{}
	for _, row := range rows {
		channel := ClassifyChannel(row.Source, row.Medium, row.HasReferrer, row.Internal)
		target, ok := merged[channel]
		if !ok {
			target = &ChannelRow{Channel: channel}
			merged[channel] = target
		}
		target.Users += row.Users
		target.Sessions += row.Sessions
		target.ConvertedUsers += row.Converted
		target.PreviousUsers += row.PreviousUsers
	}
	out := make([]ChannelRow, 0, len(merged))
	for _, row := range merged {
		row.ConversionRate = percent(row.ConvertedUsers, row.Users)
		row.ChangePercent = percentChange(float64(row.Users), float64(row.PreviousUsers))
		row.UserSharePercent = percent(row.Users, totalUsers)
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Users == out[j].Users {
			return out[i].Channel < out[j].Channel
		}
		return out[i].Users > out[j].Users
	})
	return out
}

func frequencyLabel(bucket string) string {
	switch bucket {
	case "1":
		return "1회 방문"
	case "2-3":
		return "2~3회 방문"
	case "4-9":
		return "4~9회 방문"
	default:
		return "10회 이상 방문"
	}
}

func recencyLabel(bucket string) string {
	switch bucket {
	case "0-1":
		return "최근 1일 이내 활동"
	case "2-7":
		return "2~7일 전 활동"
	case "8-30":
		return "8~30일 전 활동"
	default:
		return "31일 이상 미활동"
	}
}

type FindingInput struct {
	Days              int
	Users             int64
	PreviousUsers     int64
	NewUsers          int64
	PreviousNewUsers  int64
	NewUserShare      float64
	SessionsPerUser   float64
	PreviousSessions  float64
	EngagementRate    float64
	PreviousEngage    float64
	ConversionRate    float64
	PreviousConvRate  float64
	AverageSeconds    float64
	PreviousSeconds   float64
	Lifecycle         []LifecycleRow
	Channels          []ChannelRow
	Landing           []LandingRow
	Devices           []DeviceRow
	Frequency         []BucketRow
	LoyalNotConverted int64
	SingleVisitNew    int64
	LapsedUsers       int64
	ReturnedUsers     int64
}

// BuildFindings turns the aggregates into ranked, actionable statements.
// Every rule states the evidence it used so an analyst can verify the claim.
func BuildFindings(in FindingInput) []Finding {
	findings := []Finding{}
	add := func(f Finding) { findings = append(findings, f) }

	visitorChange := percentChange(float64(in.Users), float64(in.PreviousUsers))
	if in.PreviousUsers > 0 && math.Abs(visitorChange) >= 10 {
		severity, direction := "warning", "감소"
		if visitorChange > 0 {
			severity, direction = "positive", "증가"
		}
		cause := "채널별 증감을 확인하십시오."
		if driver := biggestChannelMover(in.Channels, visitorChange > 0); driver != "" {
			cause = driver
		}
		add(Finding{
			ID: "visitor_change", Title: fmt.Sprintf("방문자가 전기간 대비 %.1f%% %s", math.Abs(visitorChange), direction),
			Severity: severity,
			Evidence: fmt.Sprintf("최근 %d일 %d명, 이전 동일 기간 %d명", in.Days, in.Users, in.PreviousUsers),
			Cause:    cause,
			Action:   "변화가 큰 채널과 진입 페이지를 먼저 확인하고, 배포·공지 일정과 Change Calendar를 대조하십시오.",
			Impact:   math.Abs(visitorChange) + 10,
		})
	}

	if newRow, returningRow, ok := lifecycleSplit(in.Lifecycle); ok {
		if returningRow.ConversionRate > 0 && newRow.ConversionRate > 0 {
			gap := returningRow.ConversionRate - newRow.ConversionRate
			if gap >= 5 {
				add(Finding{
					ID: "onboarding_gap", Title: "신규 방문자의 전환율이 재방문자보다 낮습니다",
					Severity: "warning",
					Evidence: fmt.Sprintf("신규 %.1f%%, 재방문 %.1f%% (%.1fpp 차이)", newRow.ConversionRate, returningRow.ConversionRate, gap),
					Cause:    "첫 방문 경험에서 목표 행동까지의 경로가 길거나 안내가 부족할 수 있습니다.",
					Action:   "신규 사용자만 남긴 Segment로 Funnel과 진입 페이지를 비교해 첫 세션의 이탈 단계를 찾으십시오.",
					Impact:   gap * 2,
				})
			} else if gap <= -5 {
				add(Finding{
					ID: "retention_gap", Title: "재방문자의 전환율이 신규보다 낮습니다",
					Severity: "warning",
					Evidence: fmt.Sprintf("신규 %.1f%%, 재방문 %.1f%%", newRow.ConversionRate, returningRow.ConversionRate),
					Cause:    "재방문 목적이 전환이 아닌 조회·확인 업무일 수 있습니다.",
					Action:   "재방문 사용자의 주요 이벤트와 진입 페이지를 확인해 반복 업무를 별도 목표로 정의하십시오.",
					Impact:   math.Abs(gap) * 1.5,
				})
			}
		}
		if in.NewUserShare >= 70 && in.Users >= 20 {
			add(Finding{
				ID: "new_heavy", Title: "방문자 대부분이 신규입니다",
				Severity: "info",
				Evidence: fmt.Sprintf("신규 비중 %.1f%% (%d명 중 %d명)", in.NewUserShare, in.Users, in.NewUsers),
				Cause:    "재방문이 형성되지 않아 사용자가 반복 사용에 이르지 못하고 있습니다.",
				Action:   "Cohort/Retention에서 주차별 재방문율을 확인하고, 첫 세션 이후 다시 찾는 이유를 만드는 기능을 점검하십시오.",
				Impact:   in.NewUserShare * 0.6,
			})
		} else if in.NewUserShare <= 15 && in.Users >= 20 {
			add(Finding{
				ID: "new_starved", Title: "신규 방문자 유입이 정체되어 있습니다",
				Severity: "warning",
				Evidence: fmt.Sprintf("신규 비중 %.1f%% (%d명)", in.NewUserShare, in.NewUsers),
				Cause:    "기존 사용자만 반복 방문하고 새 대상 조직·부서로 확산되지 않았습니다.",
				Action:   "Adoption에서 미사용 조직·부서를 찾아 공지·교육 대상을 지정하십시오.",
				Impact:   60,
			})
		}
	}

	if in.PreviousEngage > 0 {
		change := in.EngagementRate - in.PreviousEngage
		if change <= -5 {
			add(Finding{
				ID: "engagement_drop", Title: "참여 세션 비율이 하락했습니다",
				Severity: "warning",
				Evidence: fmt.Sprintf("참여율 %.1f%% (이전 %.1f%%, %.1fpp 하락)", in.EngagementRate, in.PreviousEngage, math.Abs(change)),
				Cause:    "진입 후 바로 이탈하는 세션이 늘었거나 오류·성능 저하가 있을 수 있습니다.",
				Action:   "Experience에서 오류·Web Vitals를, 진입 페이지 표에서 이탈률 상위 페이지를 확인하십시오.",
				Impact:   math.Abs(change) * 2.5,
			})
		}
	}

	if worst, ok := worstLandingPage(in.Landing); ok {
		add(Finding{
			ID: "landing_bounce", Title: "이탈률이 높은 주요 진입 페이지가 있습니다",
			Severity: "warning",
			Evidence: fmt.Sprintf("%s · 세션 %d건(비중 %.1f%%) · 이탈률 %.1f%%", worst.Page, worst.Sessions, worst.SessionShare, worst.BounceRate),
			Cause:    "진입 의도와 페이지 내용이 어긋나거나 다음 행동이 명확하지 않습니다.",
			Action:   "해당 페이지의 첫 화면에 다음 행동을 명시하고, 유입 채널의 링크 대상이 올바른지 확인하십시오.",
			Impact:   worst.BounceRate*0.8 + worst.SessionShare,
		})
	}

	if gapDevice, reference, ok := deviceConversionGap(in.Devices); ok {
		add(Finding{
			ID: "device_gap", Title: fmt.Sprintf("%s 사용자의 전환율이 현저히 낮습니다", gapDevice.Device),
			Severity: "warning",
			Evidence: fmt.Sprintf("%s %.1f%% vs %s %.1f%% · %s 세션 비중 %.1f%%", gapDevice.Device, gapDevice.ConversionRate, reference.Device, reference.ConversionRate, gapDevice.Device, gapDevice.SharePercent),
			Cause:    "해당 기기의 화면·입력 환경에서 전환 경로가 불편할 수 있습니다.",
			Action:   "해당 기기 Segment로 Funnel을 비교해 이탈 단계를 찾고 화면을 점검하십시오.",
			Impact:   (reference.ConversionRate - gapDevice.ConversionRate) + gapDevice.SharePercent,
		})
	}

	if best, worst, ok := channelConversionSpread(in.Channels); ok {
		add(Finding{
			ID: "channel_spread", Title: fmt.Sprintf("%s 채널의 전환율이 가장 높습니다", best.Channel),
			Severity: "positive",
			Evidence: fmt.Sprintf("%s %.1f%% (사용자 %d명) vs %s %.1f%% (사용자 %d명)", best.Channel, best.ConversionRate, best.Users, worst.Channel, worst.ConversionRate, worst.Users),
			Cause:    "채널별 방문 의도와 진입 지점이 다릅니다.",
			Action:   fmt.Sprintf("%s의 진입 경로와 안내 문구를 %s에도 적용하고, 공지·링크 배치를 재조정하십시오.", best.Channel, worst.Channel),
			Impact:   best.ConversionRate - worst.ConversionRate,
		})
	}

	if in.LoyalNotConverted > 0 && in.Users > 0 {
		share := percent(in.LoyalNotConverted, in.Users)
		add(Finding{
			ID: "loyal_not_converted", Title: "자주 방문하지만 전환하지 않는 사용자가 있습니다",
			Severity: "warning",
			Evidence: fmt.Sprintf("3회 이상 방문하고 전환이 없는 사용자 %d명(전체의 %.1f%%)", in.LoyalNotConverted, share),
			Cause:    "관심은 확인되었으나 목표 행동을 막는 장애 요소가 남아 있습니다.",
			Action:   "이 조건으로 Segment를 만들고 Funnel·Frustration에서 반복 실패 지점을 확인한 뒤 Action으로 담당자에게 전달하십시오.",
			Impact:   share + 15,
		})
	}

	if in.LapsedUsers > 0 && in.PreviousUsers > 0 {
		share := percent(in.LapsedUsers, in.PreviousUsers)
		severity := "info"
		if share >= 40 {
			severity = "warning"
		}
		add(Finding{
			ID: "lapsed", Title: "이전 기간에만 활동한 사용자가 있습니다",
			Severity: severity,
			Evidence: fmt.Sprintf("이전 기간 활동자 중 %d명(%.1f%%)이 이번 기간에 활동하지 않았습니다. 복귀 %d명", in.LapsedUsers, share, in.ReturnedUsers),
			Cause:    "업무 주기 변화이거나 대체 수단으로 이동했을 수 있습니다.",
			Action:   "휴면 Segment를 만들어 복귀 안내를 보내고, 복귀 사용자의 첫 행동을 비교해 재방문 유도 요인을 찾으십시오.",
			Impact:   share,
		})
	}

	if in.SingleVisitNew > 0 && in.NewUsers > 0 {
		share := percent(in.SingleVisitNew, in.NewUsers)
		if share >= 60 {
			add(Finding{
				ID: "single_visit_new", Title: "신규 방문자의 재방문이 형성되지 않았습니다",
				Severity: "warning",
				Evidence: fmt.Sprintf("신규 %d명 중 %d명(%.1f%%)이 한 번만 방문했습니다", in.NewUsers, in.SingleVisitNew, share),
				Cause:    "첫 세션에서 가치를 확인하지 못했거나 다시 찾을 계기가 없습니다.",
				Action:   "첫 세션의 진입 페이지와 첫 이벤트를 확인해 두 번째 방문을 유도할 지점을 정의하십시오.",
				Impact:   share * 0.7,
			})
		}
	}

	if len(findings) == 0 {
		add(Finding{
			ID: "stable", Title: "주요 방문자 지표가 안정적입니다",
			Severity: "positive",
			Evidence: fmt.Sprintf("방문자 %d명, 참여율 %.1f%%, 전환율 %.1f%%로 전기간과 큰 차이가 없습니다", in.Users, in.EngagementRate, in.ConversionRate),
			Cause:    "감지된 급격한 변화가 없습니다.",
			Action:   "Metric Goal을 설정해 목표 대비 진행을 추적하고, Scheduled Report로 정기 배달을 설정하십시오.",
			Impact:   1,
		})
	}
	sort.SliceStable(findings, func(i, j int) bool { return findings[i].Impact > findings[j].Impact })
	return findings
}

func lifecycleSplit(rows []LifecycleRow) (LifecycleRow, LifecycleRow, bool) {
	var newRow, returningRow LifecycleRow
	foundNew, foundReturning := false, false
	for _, row := range rows {
		switch row.Kind {
		case "new":
			newRow, foundNew = row, true
		case "returning":
			returningRow, foundReturning = row, true
		}
	}
	return newRow, returningRow, foundNew && foundReturning
}

// biggestChannelMover names the channel that explains most of a visitor change.
func biggestChannelMover(channels []ChannelRow, growing bool) string {
	best := ""
	bestDelta := int64(0)
	for _, channel := range channels {
		delta := channel.Users - channel.PreviousUsers
		if !growing {
			delta = -delta
		}
		if delta > bestDelta {
			bestDelta, best = delta, channel.Channel
		}
	}
	if best == "" {
		return ""
	}
	direction := "증가"
	if !growing {
		direction = "감소"
	}
	return fmt.Sprintf("%s 채널이 %d명 %s해 변화를 주도했습니다.", best, bestDelta, direction)
}

// worstLandingPage returns a meaningful entry page with an unusually high bounce
// rate. Pages with a negligible share are ignored so the finding stays actionable.
func worstLandingPage(rows []LandingRow) (LandingRow, bool) {
	var worst LandingRow
	found := false
	for _, row := range rows {
		if row.SessionShare < 5 || row.Sessions < 5 || row.BounceRate < 60 {
			continue
		}
		if !found || row.BounceRate*row.SessionShare > worst.BounceRate*worst.SessionShare {
			worst, found = row, true
		}
	}
	return worst, found
}

// deviceConversionGap finds a device that converts far worse than the best device
// while still carrying real traffic.
func deviceConversionGap(rows []DeviceRow) (DeviceRow, DeviceRow, bool) {
	var reference DeviceRow
	for _, row := range rows {
		if row.Sessions >= 5 && row.ConversionRate > reference.ConversionRate {
			reference = row
		}
	}
	if reference.ConversionRate <= 0 {
		return DeviceRow{}, DeviceRow{}, false
	}
	var worst DeviceRow
	found := false
	for _, row := range rows {
		if row.Device == reference.Device || row.SharePercent < 10 || row.Sessions < 5 {
			continue
		}
		if row.ConversionRate > reference.ConversionRate*0.6 {
			continue
		}
		if !found || row.ConversionRate < worst.ConversionRate {
			worst, found = row, true
		}
	}
	return worst, reference, found
}

// channelConversionSpread compares the best and worst converting channels that
// both carry enough users to be worth acting on.
func channelConversionSpread(rows []ChannelRow) (ChannelRow, ChannelRow, bool) {
	eligible := []ChannelRow{}
	for _, row := range rows {
		if row.Users >= 10 {
			eligible = append(eligible, row)
		}
	}
	if len(eligible) < 2 {
		return ChannelRow{}, ChannelRow{}, false
	}
	best, worst := eligible[0], eligible[0]
	for _, row := range eligible {
		if row.ConversionRate > best.ConversionRate {
			best = row
		}
		if row.ConversionRate < worst.ConversionRate {
			worst = row
		}
	}
	if best.ConversionRate-worst.ConversionRate < 10 {
		return ChannelRow{}, ChannelRow{}, false
	}
	return best, worst, true
}

func Headline(days int, users int64, change, newShare, engagement, conversion float64) string {
	trend := "전기간과 유사"
	if change >= 1 {
		trend = fmt.Sprintf("전기간 대비 +%.1f%%", change)
	} else if change <= -1 {
		trend = fmt.Sprintf("전기간 대비 %.1f%%", change)
	}
	return fmt.Sprintf("최근 %d일 방문자 %d명(%s) · 신규 %.1f%% · 참여율 %.1f%% · 전환율 %.1f%%",
		days, users, trend, newShare, engagement, conversion)
}

func (rep Reporter) Build(ctx context.Context, siteID uuid.UUID, environment string, from, to, previousFrom, previousTo time.Time) (map[string]any, error) {
	days := int(math.Round(to.Sub(from).Hours() / 24))
	if days < 1 {
		days = 1
	}

	// Every read below is independent, so they run together under a small ceiling
	// instead of making the page wait for the sum of all eight.
	var (
		current, previous  periodMetrics
		lifecycle          []LifecycleRow
		channelSources     []channelSource
		landing            []LandingRow
		frequency, recency []BucketRow
		signals            map[string]int64
		deviceRows         []DeviceRow
		lapsed, returned   int64
	)
	err := RunParallel(ctx, QueryConcurrency,
		func(ctx context.Context) error {
			value, err := rep.periodMetrics(ctx, siteID, environment, from, to)
			current = value
			return err
		},
		func(ctx context.Context) error {
			// The previous period is context, not the answer, so a gap in history
			// must not fail the whole report.
			previous, _ = rep.periodMetrics(ctx, siteID, environment, previousFrom, previousTo)
			return nil
		},
		func(ctx context.Context) error {
			value, err := rep.insightLifecycle(ctx, siteID, environment, from, to)
			lifecycle = value
			return err
		},
		func(ctx context.Context) error {
			value, err := rep.insightChannelSources(ctx, siteID, environment, from, to, previousFrom)
			channelSources = value
			return err
		},
		func(ctx context.Context) error {
			value, err := rep.insightLandingPages(ctx, siteID, environment, from, to)
			landing = value
			return err
		},
		func(ctx context.Context) error {
			buckets, recent, counts, err := rep.insightVisitorBuckets(ctx, siteID, environment, from, to)
			frequency, recency, signals = buckets, recent, counts
			return err
		},
		func(ctx context.Context) error {
			value, err := rep.insightDeviceRows(ctx, siteID, environment, from, to)
			deviceRows = value
			return err
		},
		func(ctx context.Context) error {
			absent, back, err := rep.insightRetentionFlow(ctx, siteID, environment, from, to, previousFrom, previousTo)
			lapsed, returned = absent, back
			return err
		},
	)
	if err != nil {
		return nil, err
	}
	// Shares depend on the visitor total, so they are derived once every read is in
	// rather than threaded through the queries.
	channels := GroupChannels(channelSources, current.Users)
	devices := withDeviceShare(deviceRows, current.Users)

	newShare := current.newUserShare()
	previousNewShare := previous.newUserShare()
	sessionsPerUser := ratio(float64(current.Sessions), float64(current.Users))
	previousSessionsPerUser := ratio(float64(previous.Sessions), float64(previous.Users))
	pagesPerSession := ratio(float64(current.PageViews), float64(current.Sessions))
	previousPagesPerSession := ratio(float64(previous.PageViews), float64(previous.Sessions))

	kpis := []KPI{
		kpi("users", "방문자", "number", float64(current.Users), float64(previous.Users), "higher"),
		kpi("new_users", "신규 방문자", "number", float64(current.NewUsers), float64(previous.NewUsers), "higher"),
		kpi("returning_users", "재방문자", "number", float64(current.returningUsers()), float64(previous.returningUsers()), "higher"),
		kpi("new_user_share", "신규 비중", "percent", newShare, previousNewShare, "neutral"),
		kpi("sessions", "세션", "number", float64(current.Sessions), float64(previous.Sessions), "higher"),
		kpi("sessions_per_user", "1인당 방문 횟수", "decimal", sessionsPerUser, previousSessionsPerUser, "higher"),
		kpi("pages_per_session", "세션당 페이지뷰", "decimal", pagesPerSession, previousPagesPerSession, "higher"),
		kpi("engagement_rate", "참여율", "percent", current.engagementRate(), previous.engagementRate(), "higher"),
		kpi("avg_session_duration", "평균 체류 시간", "duration", current.AverageSeconds, previous.AverageSeconds, "higher"),
		kpi("user_conversion_rate", "사용자 전환율", "percent", current.conversionRate(), previous.conversionRate(), "higher"),
	}

	findings := BuildFindings(FindingInput{
		Days:              days,
		Users:             current.Users,
		PreviousUsers:     previous.Users,
		NewUsers:          current.NewUsers,
		PreviousNewUsers:  previous.NewUsers,
		NewUserShare:      newShare,
		SessionsPerUser:   sessionsPerUser,
		PreviousSessions:  previousSessionsPerUser,
		EngagementRate:    current.engagementRate(),
		PreviousEngage:    previous.engagementRate(),
		ConversionRate:    current.conversionRate(),
		PreviousConvRate:  previous.conversionRate(),
		AverageSeconds:    current.AverageSeconds,
		PreviousSeconds:   previous.AverageSeconds,
		Lifecycle:         lifecycle,
		Channels:          channels,
		Landing:           landing,
		Devices:           devices,
		Frequency:         frequency,
		LoyalNotConverted: signals["loyal_not_converted"],
		SingleVisitNew:    signals["single_visit_new"],
		LapsedUsers:       lapsed,
		ReturnedUsers:     returned,
	})

	return map[string]any{
		"environment":   environment,
		"from":          from,
		"to":            to,
		"previous_from": previousFrom,
		"previous_to":   previousTo,
		"days":          days,
		"headline":      Headline(days, current.Users, percentChange(float64(current.Users), float64(previous.Users)), newShare, current.engagementRate(), current.conversionRate()),
		"kpis":          kpis,
		"findings":      findings,
		"lifecycle":     lifecycle,
		"channels":      channels,
		"landing_pages": landing,
		"frequency":     frequency,
		"recency":       recency,
		"devices":       devices,
		"audiences":     audiences(days, signals, lapsed, returned),
		"notes": []string{
			"채널·기기별 사용자 합계는 한 사용자가 여러 채널로 방문하면 중복 계산될 수 있습니다.",
			"신규 여부는 선택한 환경의 전체 수집 이력에서 처음 관측된 시점으로 판단합니다.",
			"익명 방문자는 사이트 범위로 격리하고, SSO User ID가 있으면 사이트를 넘어 한 사람으로 계산합니다.",
		},
	}, nil
}

// segmentRule mirrors the segment definition the API accepts so an audience can be
// saved as a real, re-usable segment instead of being retyped by hand.
type segmentRule struct {
	Combinator string        `json:"combinator,omitempty"`
	Rules      []segmentRule `json:"rules,omitempty"`
	Field      string        `json:"field,omitempty"`
	Operator   string        `json:"operator,omitempty"`
	Value      any           `json:"value,omitempty"`
}

func and(rules ...segmentRule) segmentRule {
	return segmentRule{Combinator: "and", Rules: rules}
}

func rule(field, operator string, value any) segmentRule {
	return segmentRule{Field: field, Operator: operator, Value: value}
}

// audiences describes who to act on, with the exact segment definition behind each
// group. Where a definition can only approximate the counted group, it says so.
func audiences(days int, signals map[string]int64, lapsed, returned int64) []map[string]any {
	window := float64(days)
	return []map[string]any{
		{
			"key": "loyal_not_converted", "label": "3회 이상 방문했지만 미전환", "users": signals["loyal_not_converted"],
			"action":  "Funnel과 Frustration으로 장애 요인을 찾고 담당자에게 전달",
			"segment": and(rule("entity.sessions", ">=", 3), rule("entity.conversions", "=", 0)),
		},
		{
			"key": "single_visit_new", "label": "한 번만 방문한 신규", "users": signals["single_visit_new"],
			"action":       "첫 세션 진입 페이지와 첫 이벤트를 개선",
			"segment":      and(rule("entity.sessions", "<=", 1), rule("entity.days_since_first_seen", "<=", window)),
			"segment_note": "Segment는 전체 이력 기준이므로 조회 기간과 경계에서 인원이 다를 수 있습니다.",
		},
		{
			"key": "lapsed", "label": "이전 기간에만 활동(휴면)", "users": lapsed,
			"action":       "복귀 안내 발송 대상",
			"segment":      and(rule("entity.days_since_last_seen", ">=", window)),
			"segment_note": "Segment는 현재 시점 기준 휴면 일수로 계산하므로 조회 기간과 완전히 일치하지 않습니다.",
		},
		{
			"key": "returned", "label": "휴면 후 이번 기간 복귀", "users": returned,
			"action":       "복귀 계기를 확인해 재방문 유도 요인으로 확대",
			"segment":      and(rule("entity.days_since_last_seen", "<=", window), rule("entity.days_since_first_seen", ">=", window*2)),
			"segment_note": "복귀 판정은 기간 비교 결과이고 Segment는 근사값입니다. 인원이 다를 수 있습니다.",
		},
	}
}

func kpi(key, label, format string, current, previous float64, goal string) KPI {
	return KPI{Key: key, Label: label, Format: format, Current: current, Previous: previous, ChangePercent: percentChange(current, previous), Goal: goal}
}

func ratio(numerator, denominator float64) float64 {
	if denominator == 0 {
		return 0
	}
	return numerator / denominator
}

func (rep Reporter) insightLifecycle(ctx context.Context, siteID uuid.UUID, environment string, from, to time.Time) ([]LifecycleRow, error) {
	// The first-seen scan stops at the end of the period: a row after it cannot
	// move anybody's first event into the window, and reading the rest made this
	// grow with the site's whole history instead of with the period.
	rows, err := rep.DB.Query(ctx, `WITH first_seen AS (
		SELECT entity_id,min(event_timestamp) first_at FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp < $3 GROUP BY 1
	), period AS (
		SELECT entity_id,session_id,is_conversion FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3
	)
	SELECT CASE WHEN f.first_at >= $2 THEN 'new' ELSE 'returning' END,
		count(DISTINCT p.entity_id),count(DISTINCT p.session_id),count(DISTINCT p.entity_id) FILTER(WHERE p.is_conversion)
	FROM period p JOIN first_seen f ON f.entity_id=p.entity_id GROUP BY 1`, siteID, from, to, environment)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LifecycleRow{}
	total := int64(0)
	for rows.Next() {
		var row LifecycleRow
		var converted int64
		if rows.Scan(&row.Kind, &row.Users, &row.Sessions, &converted) != nil {
			continue
		}
		row.ConversionRate = percent(converted, row.Users)
		row.SessionsPerUser = ratio(float64(row.Sessions), float64(row.Users))
		total += row.Users
		out = append(out, row)
	}
	for index := range out {
		out[index].SharePercent = percent(out[index].Users, total)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out, rows.Err()
}

func (rep Reporter) insightChannelSources(ctx context.Context, siteID uuid.UUID, environment string, from, to, previousFrom time.Time) ([]channelSource, error) {
	rows, err := rep.DB.Query(ctx, `SELECT coalesce(source,''),coalesce(medium,''),coalesce(referrer,'')<>'',coalesce(network_name,'')<>'',
		count(DISTINCT entity_id) FILTER(WHERE event_timestamp >= $2),
		count(DISTINCT session_id) FILTER(WHERE event_timestamp >= $2),
		count(DISTINCT entity_id) FILTER(WHERE event_timestamp >= $2 AND is_conversion),
		count(DISTINCT entity_id) FILTER(WHERE event_timestamp < $2)
		FROM analytics_events
		WHERE site_id=$1 AND environment=$5 AND event_timestamp >= $4 AND event_timestamp < $3
		GROUP BY 1,2,3,4`, siteID, from, to, previousFrom, environment)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	source := []channelSource{}
	for rows.Next() {
		var row channelSource
		if rows.Scan(&row.Source, &row.Medium, &row.HasReferrer, &row.Internal, &row.Users, &row.Sessions, &row.Converted, &row.PreviousUsers) != nil {
			continue
		}
		source = append(source, row)
	}
	return source, rows.Err()
}

func (rep Reporter) insightLandingPages(ctx context.Context, siteID uuid.UUID, environment string, from, to time.Time) ([]LandingRow, error) {
	rows, err := rep.DB.Query(ctx, `SELECT coalesce(nullif(landing_page,''),'(not set)'),count(*),
		count(*) FILTER(WHERE page_views<=1 AND conversion_count=0 AND NOT engaged),
		count(*) FILTER(WHERE engaged),
		count(*) FILTER(WHERE conversion_count>0),
		coalesce(avg(extract(epoch FROM (last_event_at-started_at))),0)::double precision,
		(SELECT count(*) FROM sessions WHERE site_id=$1 AND environment=$4 AND started_at >= $2 AND started_at < $3)
		FROM sessions WHERE site_id=$1 AND environment=$4 AND started_at >= $2 AND started_at < $3
		GROUP BY 1 ORDER BY 2 DESC LIMIT 12`, siteID, from, to, environment)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LandingRow{}
	for rows.Next() {
		var row LandingRow
		var bounces, engaged, converted, totalSessions int64
		if rows.Scan(&row.Page, &row.Sessions, &bounces, &engaged, &converted, &row.AverageSeconds, &totalSessions) != nil {
			continue
		}
		row.BounceRate = percent(bounces, row.Sessions)
		row.EngagementRate = percent(engaged, row.Sessions)
		row.ConversionRate = percent(converted, row.Sessions)
		row.SessionShare = percent(row.Sessions, totalSessions)
		out = append(out, row)
	}
	return out, rows.Err()
}

// insightVisitorBuckets returns visit frequency and recency distributions plus the
// counts behind the actionable audiences, in a single pass over the period.
func (rep Reporter) insightVisitorBuckets(ctx context.Context, siteID uuid.UUID, environment string, from, to time.Time) ([]BucketRow, []BucketRow, map[string]int64, error) {
	// first_at used to be read with a scalar subquery inside per_user, which ran
	// once per person against an unbounded scan of the site. Grouping first and
	// joining once is the same answer in one pass.
	rows, err := rep.DB.Query(ctx, `WITH first_seen AS (
		SELECT entity_id,min(event_timestamp) first_at FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp < $3 GROUP BY 1
	), active AS (
		SELECT entity_id,count(DISTINCT session_id) sessions,max(event_timestamp) last_at,bool_or(is_conversion) converted
		FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3 GROUP BY entity_id
	), per_user AS (
		SELECT a.entity_id,a.sessions,a.last_at,a.converted,f.first_at
		FROM active a LEFT JOIN first_seen f ON f.entity_id=a.entity_id
	)
	SELECT 'frequency',CASE WHEN sessions<=1 THEN '1' WHEN sessions<=3 THEN '2-3' WHEN sessions<=9 THEN '4-9' ELSE '10+' END,
		count(*),count(*) FILTER(WHERE converted) FROM per_user GROUP BY 2
	UNION ALL
	SELECT 'recency',CASE WHEN last_at >= $3-interval '1 day' THEN '0-1' WHEN last_at >= $3-interval '7 days' THEN '2-7' WHEN last_at >= $3-interval '30 days' THEN '8-30' ELSE '31+' END,
		count(*),count(*) FILTER(WHERE converted) FROM per_user GROUP BY 2
	UNION ALL
	SELECT 'signal','loyal_not_converted',count(*),0 FROM per_user WHERE sessions>=3 AND NOT converted
	UNION ALL
	SELECT 'signal','single_visit_new',count(*),0 FROM per_user WHERE sessions<=1 AND first_at >= $2`, siteID, from, to, environment)
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()
	frequency, recency := []BucketRow{}, []BucketRow{}
	signals := map[string]int64{"loyal_not_converted": 0, "single_visit_new": 0}
	frequencyTotal, recencyTotal := int64(0), int64(0)
	for rows.Next() {
		var kind, bucket string
		var users, converted int64
		if rows.Scan(&kind, &bucket, &users, &converted) != nil {
			continue
		}
		switch kind {
		case "frequency":
			frequencyTotal += users
			frequency = append(frequency, BucketRow{Bucket: bucket, Label: frequencyLabel(bucket), Users: users, ConversionRate: percent(converted, users)})
		case "recency":
			recencyTotal += users
			recency = append(recency, BucketRow{Bucket: bucket, Label: recencyLabel(bucket), Users: users, ConversionRate: percent(converted, users)})
		case "signal":
			signals[bucket] = users
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, nil, err
	}
	order := map[string]int{"1": 0, "2-3": 1, "4-9": 2, "10+": 3, "0-1": 0, "2-7": 1, "8-30": 2, "31+": 3}
	finish := func(items []BucketRow, total int64) []BucketRow {
		for index := range items {
			items[index].SharePercent = percent(items[index].Users, total)
		}
		sort.Slice(items, func(i, j int) bool { return order[items[i].Bucket] < order[items[j].Bucket] })
		return items
	}
	return finish(frequency, frequencyTotal), finish(recency, recencyTotal), signals, nil
}

// withDeviceShare fills in each device's share of the visitor total.
func withDeviceShare(rows []DeviceRow, totalUsers int64) []DeviceRow {
	for index := range rows {
		rows[index].SharePercent = percent(rows[index].Users, totalUsers)
	}
	return rows
}

func (rep Reporter) insightDeviceRows(ctx context.Context, siteID uuid.UUID, environment string, from, to time.Time) ([]DeviceRow, error) {
	rows, err := rep.DB.Query(ctx, `SELECT coalesce(nullif(device_type,''),'unknown'),count(DISTINCT entity_id),count(DISTINCT session_id),count(DISTINCT entity_id) FILTER(WHERE is_conversion)
		FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3
		GROUP BY 1 ORDER BY 2 DESC LIMIT 8`, siteID, from, to, environment)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DeviceRow{}
	for rows.Next() {
		var row DeviceRow
		var converted int64
		if rows.Scan(&row.Device, &row.Users, &row.Sessions, &converted) != nil {
			continue
		}
		row.ConversionRate = percent(converted, row.Users)
		out = append(out, row)
	}
	return out, rows.Err()
}

// insightRetentionFlow counts users who stopped visiting and users who came back
// after being absent in the previous period.
func (rep Reporter) insightRetentionFlow(ctx context.Context, siteID uuid.UUID, environment string, from, to, previousFrom, previousTo time.Time) (int64, int64, error) {
	var lapsed, returned int64
	err := rep.DB.QueryRow(ctx, `WITH previous AS (
		SELECT DISTINCT entity_id FROM analytics_events WHERE site_id=$1 AND environment=$6 AND event_timestamp >= $4 AND event_timestamp < $5
	), current AS (
		SELECT DISTINCT entity_id FROM analytics_events WHERE site_id=$1 AND environment=$6 AND event_timestamp >= $2 AND event_timestamp < $3
	), earlier AS (
		SELECT DISTINCT entity_id FROM analytics_events WHERE site_id=$1 AND environment=$6 AND event_timestamp < $4
	)
	SELECT (SELECT count(*) FROM previous WHERE entity_id NOT IN (SELECT entity_id FROM current)),
		(SELECT count(*) FROM current WHERE entity_id NOT IN (SELECT entity_id FROM previous) AND entity_id IN (SELECT entity_id FROM earlier))`,
		siteID, from, to, previousFrom, previousTo, environment).Scan(&lapsed, &returned)
	return lapsed, returned, err
}

// periodMetrics reads the comparison aggregates for one period. Engagement and
// duration come from the materialized sessions table so they follow the
// site-configured engagement rule instead of a second definition.
func (rep Reporter) periodMetrics(ctx context.Context, siteID uuid.UUID, environment string, from, to time.Time) (periodMetrics, error) {
	var m periodMetrics
	err := rep.DB.QueryRow(ctx, `WITH period AS (
		SELECT entity_id,session_id,event_name,is_conversion FROM analytics_events
		WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3
	-- The first-seen scan stops at the end of the period for the same reason the
	-- other reports' do: a row after it cannot move anybody's first event into the
	-- window, and reading the rest made this grow with the site's whole history.
	), first_seen AS (
		SELECT count(*) value FROM (
			SELECT entity_id FROM analytics_events
			WHERE site_id=$1 AND environment=$4 AND event_timestamp < $3
			GROUP BY entity_id HAVING min(event_timestamp) >= $2
		) firsts
	), session_summary AS (
		SELECT count(*) sessions,count(*) FILTER(WHERE engaged) engaged,
			coalesce(avg(extract(epoch FROM (last_event_at-started_at))),0)::double precision average_seconds
		FROM sessions WHERE site_id=$1 AND environment=$4 AND started_at >= $2 AND started_at < $3
	), event_sessions AS (
		-- Only consulted when no session row exists for the period, which means
		-- the derived data is behind rather than that nothing happened. Taking the
		-- larger of the two would mix definitions and disagree with the overview.
		SELECT count(DISTINCT session_id) sessions FROM period
	)
	SELECT count(DISTINCT p.entity_id),
		(SELECT value FROM first_seen),
		CASE WHEN (SELECT sessions FROM session_summary) > 0
			THEN (SELECT sessions FROM session_summary)
			ELSE (SELECT sessions FROM event_sessions) END,
		count(*) FILTER(WHERE p.event_name='page_view'),
		count(DISTINCT p.entity_id) FILTER(WHERE p.is_conversion),
		(SELECT engaged FROM session_summary),
		(SELECT average_seconds FROM session_summary)
	FROM period p`, siteID, from, to, environment).
		Scan(&m.Users, &m.NewUsers, &m.Sessions, &m.PageViews, &m.ConvertedUsers, &m.EngagedSessions, &m.AverageSeconds)
	return m, err
}
