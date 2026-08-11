package insight

import (
	"strings"
	"testing"
)

func TestClassifyChannelGroupsAcquisition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                    string
		source, medium          string
		hasReferrer, internalIP bool
		want                    string
	}{
		{name: "no source is direct", want: "Direct"},
		{name: "no source on the corporate network", internalIP: true, want: "Direct (사내망)"},
		{name: "paid beats organic", source: "google", medium: "cpc", want: "Paid Search"},
		{name: "search engine without medium", source: "naver", want: "Organic Search"},
		{name: "explicit organic", source: "internal-search", medium: "organic", want: "Organic Search"},
		{name: "mail gateway", source: "mailgateway", medium: "email", want: "Email"},
		{name: "social", source: "kakao", medium: "social", want: "Social"},
		{name: "notice", source: "portal", medium: "notice", want: "Internal Notice"},
		{name: "internal portal", source: "groupware", medium: "link", want: "Internal Portal"},
		{name: "messenger", source: "teams", medium: "messenger", want: "Internal Message"},
		{name: "referrer only", hasReferrer: true, want: "Referral"},
		{name: "unknown medium", source: "vendor", medium: "partner", want: "Other"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyChannel(tt.source, tt.medium, tt.hasReferrer, tt.internalIP); got != tt.want {
				t.Fatalf("ClassifyChannel(%q,%q,%v,%v) = %q, want %q", tt.source, tt.medium, tt.hasReferrer, tt.internalIP, got, tt.want)
			}
		})
	}
}

func TestGroupChannelsMergesSourcesAndRanks(t *testing.T) {
	t.Parallel()

	rows := []channelSource{
		{Source: "naver", Medium: "organic", Users: 30, Sessions: 40, Converted: 3, PreviousUsers: 20},
		{Source: "google", Medium: "organic", Users: 10, Sessions: 12, Converted: 2, PreviousUsers: 5},
		{Source: "", Medium: "", Users: 50, Sessions: 70, Converted: 10, PreviousUsers: 60},
	}
	got := GroupChannels(rows, 90)
	if len(got) != 2 {
		t.Fatalf("channels = %d, want 2", len(got))
	}
	if got[0].Channel != "Direct" || got[0].Users != 50 {
		t.Fatalf("first channel = %+v, want Direct with 50 users", got[0])
	}
	if got[1].Channel != "Organic Search" || got[1].Users != 40 || got[1].Sessions != 52 {
		t.Fatalf("second channel = %+v, want merged Organic Search", got[1])
	}
	if got[1].ConversionRate < 12.4 || got[1].ConversionRate > 12.6 {
		t.Fatalf("organic conversion rate = %.2f, want 12.5", got[1].ConversionRate)
	}
	if got[1].ChangePercent < 59 || got[1].ChangePercent > 61 {
		t.Fatalf("organic change = %.2f, want 60", got[1].ChangePercent)
	}
	if got[0].UserSharePercent < 55 || got[0].UserSharePercent > 56 {
		t.Fatalf("direct share = %.2f, want 55.6", got[0].UserSharePercent)
	}
}

func findingByID(findings []Finding, id string) (Finding, bool) {
	for _, finding := range findings {
		if finding.ID == id {
			return finding, true
		}
	}
	return Finding{}, false
}

func TestBuildFindingsExplainsVisitorDropWithItsDriver(t *testing.T) {
	t.Parallel()

	findings := BuildFindings(FindingInput{
		Days: 30, Users: 700, PreviousUsers: 1000,
		Channels: []ChannelRow{
			{Channel: "Internal Portal", Users: 300, PreviousUsers: 600},
			{Channel: "Direct", Users: 400, PreviousUsers: 400},
		},
	})
	finding, ok := findingByID(findings, "visitor_change")
	if !ok {
		t.Fatalf("visitor_change finding is missing from %+v", findings)
	}
	if finding.Severity != "warning" {
		t.Fatalf("severity = %q, want warning", finding.Severity)
	}
	if !strings.Contains(finding.Title, "30.0%") || !strings.Contains(finding.Title, "감소") {
		t.Fatalf("title = %q, want the 30%% drop", finding.Title)
	}
	if !strings.Contains(finding.Cause, "Internal Portal") {
		t.Fatalf("cause = %q, want the driving channel", finding.Cause)
	}
	if findings[0].ID != "visitor_change" {
		t.Fatalf("highest impact finding = %q, want visitor_change", findings[0].ID)
	}
}

func TestBuildFindingsDetectsOnboardingGap(t *testing.T) {
	t.Parallel()

	findings := BuildFindings(FindingInput{
		Days: 7, Users: 200, PreviousUsers: 200, NewUsers: 80, NewUserShare: 40,
		Lifecycle: []LifecycleRow{
			{Kind: "new", Users: 80, ConversionRate: 4},
			{Kind: "returning", Users: 120, ConversionRate: 18},
		},
	})
	finding, ok := findingByID(findings, "onboarding_gap")
	if !ok {
		t.Fatalf("onboarding_gap finding is missing from %+v", findings)
	}
	if !strings.Contains(finding.Evidence, "14.0pp") {
		t.Fatalf("evidence = %q, want the point gap", finding.Evidence)
	}
}

func TestBuildFindingsIgnoresMarginalLandingPages(t *testing.T) {
	t.Parallel()

	// A high bounce rate on a page with 2% of sessions is not worth an action.
	findings := BuildFindings(FindingInput{
		Days: 30, Users: 100, PreviousUsers: 100,
		Landing: []LandingRow{{Page: "/rare", Sessions: 6, BounceRate: 95, SessionShare: 2}},
	})
	if _, ok := findingByID(findings, "landing_bounce"); ok {
		t.Fatal("a marginal landing page produced a finding")
	}

	findings = BuildFindings(FindingInput{
		Days: 30, Users: 100, PreviousUsers: 100,
		Landing: []LandingRow{{Page: "/search", Sessions: 400, BounceRate: 82, SessionShare: 45}},
	})
	finding, ok := findingByID(findings, "landing_bounce")
	if !ok {
		t.Fatalf("a dominant high-bounce page produced no finding: %+v", findings)
	}
	if !strings.Contains(finding.Evidence, "/search") {
		t.Fatalf("evidence = %q, want the page", finding.Evidence)
	}
}

func TestDeviceConversionGapNeedsRealTraffic(t *testing.T) {
	t.Parallel()

	// Mobile converts at a third of desktop and carries a quarter of the users.
	gap, reference, ok := deviceConversionGap([]DeviceRow{
		{Device: "desktop", Users: 300, Sessions: 400, ConversionRate: 15, SharePercent: 75},
		{Device: "mobile", Users: 100, Sessions: 120, ConversionRate: 5, SharePercent: 25},
	})
	if !ok || gap.Device != "mobile" || reference.Device != "desktop" {
		t.Fatalf("gap = %+v, reference = %+v, ok = %v", gap, reference, ok)
	}

	// The same ratio on a negligible share is not actionable.
	if _, _, ok := deviceConversionGap([]DeviceRow{
		{Device: "desktop", Users: 300, Sessions: 400, ConversionRate: 15, SharePercent: 97},
		{Device: "tablet", Users: 6, Sessions: 6, ConversionRate: 0, SharePercent: 3},
	}); ok {
		t.Fatal("a 3% share device produced a gap finding")
	}
}

func TestChannelConversionSpreadRequiresAMeaningfulDifference(t *testing.T) {
	t.Parallel()

	if _, _, ok := channelConversionSpread([]ChannelRow{
		{Channel: "Direct", Users: 100, ConversionRate: 12},
		{Channel: "Referral", Users: 50, ConversionRate: 9},
	}); ok {
		t.Fatal("a 3pp spread was reported")
	}
	best, worst, ok := channelConversionSpread([]ChannelRow{
		{Channel: "Direct", Users: 100, ConversionRate: 8},
		{Channel: "Internal Notice", Users: 40, ConversionRate: 30},
		{Channel: "Tiny", Users: 3, ConversionRate: 99},
	})
	if !ok || best.Channel != "Internal Notice" || worst.Channel != "Direct" {
		t.Fatalf("best = %+v, worst = %+v, ok = %v", best, worst, ok)
	}
}

func TestBuildFindingsAlwaysReturnsSomething(t *testing.T) {
	t.Parallel()

	findings := BuildFindings(FindingInput{Days: 30, Users: 10, PreviousUsers: 10, EngagementRate: 50, ConversionRate: 5})
	if len(findings) != 1 || findings[0].ID != "stable" {
		t.Fatalf("findings = %+v, want a single stable finding", findings)
	}
	if findings[0].Severity != "positive" {
		t.Fatalf("severity = %q, want positive", findings[0].Severity)
	}
}

func TestHeadlineStatesTheComparison(t *testing.T) {
	t.Parallel()

	got := Headline(30, 1234, 12.34, 45.6, 62.1, 8.4)
	for _, want := range []string{"최근 30일", "1234명", "+12.3%", "신규 45.6%", "참여율 62.1%", "전환율 8.4%"} {
		if !strings.Contains(got, want) {
			t.Fatalf("headline %q is missing %q", got, want)
		}
	}
	if !strings.Contains(Headline(7, 10, 0.2, 10, 10, 10), "전기간과 유사") {
		t.Fatalf("a flat period should read as unchanged: %q", Headline(7, 10, 0.2, 10, 10, 10))
	}
}

func TestBucketLabelsCoverEveryBucket(t *testing.T) {
	t.Parallel()

	for bucket, want := range map[string]string{"1": "1회 방문", "2-3": "2~3회 방문", "4-9": "4~9회 방문", "10+": "10회 이상 방문"} {
		if got := frequencyLabel(bucket); got != want {
			t.Fatalf("frequencyLabel(%q) = %q, want %q", bucket, got, want)
		}
	}
	for bucket, want := range map[string]string{"0-1": "최근 1일 이내 활동", "2-7": "2~7일 전 활동", "8-30": "8~30일 전 활동", "31+": "31일 이상 미활동"} {
		if got := recencyLabel(bucket); got != want {
			t.Fatalf("recencyLabel(%q) = %q, want %q", bucket, got, want)
		}
	}
}

func TestPercentChangeHandlesAZeroBaseline(t *testing.T) {
	t.Parallel()

	if got := percentChange(0, 0); got != 0 {
		t.Fatalf("percentChange(0,0) = %v, want 0", got)
	}
	if got := percentChange(5, 0); got != 100 {
		t.Fatalf("percentChange(5,0) = %v, want 100", got)
	}
	if got := percentChange(50, 100); got != -50 {
		t.Fatalf("percentChange(50,100) = %v, want -50", got)
	}
}
