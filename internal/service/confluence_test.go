package service

import (
	"strings"
	"testing"
	"time"
)

// The Confluence channel is the only delivery whose rendering Momento owns: a
// webhook hands the payload to something else, a Confluence page is the finished
// artefact a team reads. It published the payload as JSON in a <pre> block.
//
// These hold the page to being a page. They are written against what a reader
// has to be able to see — the figures, what they were before, which way they
// moved — rather than against the markup, so the rendering can be rewritten
// without rewriting them.
func TestTheConfluencePageIsAPageAndNotAJSONDump(t *testing.T) {
	from := time.Date(2026, 8, 20, 15, 0, 0, 0, time.UTC)
	page := confluenceBody(map[string]any{
		"name": "주간 개요", "kind": "insights", "environment": "prd", "site_id": "SITE_MAIN",
		"from": from, "to": from.AddDate(0, 0, 7),
		"data": map[string]any{
			"current":        map[string]any{"users": int64(12043), "revenue": 87000.0, "conversions": int64(90)},
			"previous":       map[string]any{"users": int64(9000), "revenue": 90000.0, "conversions": int64(120)},
			"change_percent": map[string]any{"users": 33.81, "revenue": -3.33, "conversions": -25.0},
			"insights": []any{map[string]any{
				"title": "전환 변화", "severity": "critical", "change_percent": -25.0,
				"current": 90.0, "previous": 120.0, "recommendation": "이탈이 커진 단계를 확인하세요.",
			}},
		},
	})

	// A reader has to be able to see the number, what it was, and which way it
	// went, without reading JSON.
	for _, want := range []string{"12,043", "9,000", "▲ 33.8%", "▼ 25.0%", "▼ 3.3%"} {
		if !strings.Contains(page, want) {
			t.Errorf("the page does not show %q:\n%s", want, page)
		}
	}
	if strings.Contains(page, "1.2043e+04") || strings.Contains(page, "12043.00") {
		t.Errorf("a count is printed the way Go marshals it rather than the way it is read:\n%s", page)
	}
	// The ranked findings are the reason this report exists.
	for _, want := range []string{"주목할 변화", "전환 변화", "critical", "이탈이 커진 단계를 확인하세요."} {
		if !strings.Contains(page, want) {
			t.Errorf("the page does not carry the insight %q:\n%s", want, page)
		}
	}
	// It is a table, not a preformatted block with everything in it.
	if !strings.Contains(page, "<table>") {
		t.Errorf("the page has no table in it:\n%s", page)
	}
	// The machine readable copy is kept, behind a disclosure rather than as the
	// body: teams that parse the page must not lose anything.
	if !strings.Contains(page, `ac:name="expand"`) {
		t.Fatalf("the original document is not attached at all:\n%s", page)
	}
	attached := page[strings.Index(page, `ac:name="expand"`):]
	// The document is escaped inside the macro, so the check is on what it says
	// rather than on the exact quoting: it has to still carry every field.
	for _, field := range []string{"kind", "insights", "change_percent", "site_id"} {
		if !strings.Contains(attached, field) {
			t.Errorf("the attached document has lost %q — a team parsing the page gets less than the webhook would send:\n%s", field, attached)
		}
	}
	if strings.Index(page, "<table>") > strings.Index(page, `ac:name="expand"`) {
		t.Error("the raw document comes before the report, so the page still reads as a dump")
	}
}

// Whatever reaches the page came from an event property, which is whatever the
// site sent. Confluence storage format is XHTML, so an unescaped value is markup.
func TestTheConfluencePageEscapesWhatTheSiteSent(t *testing.T) {
	page := confluenceBody(map[string]any{
		"name": `주간 <script>alert(1)</script>`, "kind": "ai",
		"data": map[string]any{"rows": []any{map[string]any{
			"label": `<ac:structured-macro ac:name="html"/>`, "calls": int64(3),
		}}},
	})
	if strings.Contains(page, "<script>") {
		t.Errorf("a report name became markup on the page:\n%s", page)
	}
	if strings.Contains(page, `<ac:structured-macro ac:name="html"`) {
		t.Errorf("an event property became a Confluence macro:\n%s", page)
	}
	if !strings.Contains(page, "&lt;script&gt;") {
		t.Errorf("the name is not on the page at all, escaped or otherwise:\n%s", page)
	}
}

// A report with nothing in it still has to publish something a reader can act
// on. A blank page reads as a broken delivery.
func TestTheConfluencePageSaysWhenThereIsNothingToReport(t *testing.T) {
	page := confluenceBody(map[string]any{"name": "AI 사용량", "kind": "ai",
		"data": map[string]any{"rows": []any{}}})
	if !strings.Contains(page, "기록된 항목이 없습니다") {
		t.Errorf("an empty report publishes a page that says nothing:\n%s", page)
	}
	empty := confluenceBody(map[string]any{"name": "비어 있음", "kind": "overview"})
	if !strings.Contains(empty, "내용이 없습니다") {
		t.Errorf("a delivery with no data publishes a blank page:\n%s", empty)
	}
}

// The visitor insight report is the richest thing Momento delivers — a sentence,
// the KPIs against the previous period, and findings that each state their
// evidence and the action they imply. It arrived as the largest block of JSON on
// the page.
func TestTheConfluencePageRendersTheVisitorInsightReport(t *testing.T) {
	page := confluenceBody(map[string]any{
		"name": "방문자 인사이트", "kind": "visitor_insight",
		"data": map[string]any{
			"headline": "지난 30일 방문자 12,043명, 이전 기간 대비 33.8% 증가했습니다.",
			"kpis": []any{
				map[string]any{"key": "users", "label": "방문자", "current": 12043.0, "previous": 9000.0, "change_percent": 33.81},
				map[string]any{"key": "engagement_rate", "label": "참여율", "current": 41.2, "previous": 47.9, "change_percent": -13.99},
			},
			"findings": []any{map[string]any{
				"title": "참여율 하락", "severity": "warning",
				"evidence": "41.2%에서 47.9%로", "action": "진입 페이지별 이탈을 확인하세요.",
			}},
		},
	})
	for _, want := range []string{"지난 30일 방문자", "방문자", "12,043", "▼ 14.0%", "발견", "참여율 하락", "진입 페이지별 이탈을 확인하세요."} {
		if !strings.Contains(page, want) {
			t.Errorf("the page does not carry %q:\n%s", want, page)
		}
	}
}
