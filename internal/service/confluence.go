package service

import (
	"encoding/json"
	"fmt"
	"html"
	"sort"
	"strings"
	"time"
)

// The Confluence channel is the one delivery Momento renders itself. A webhook,
// a mail gateway and an AI agent endpoint all receive the payload and decide
// what to do with it; a Confluence page is the finished artefact, published
// where a team reads it.
//
// It published the payload as JSON inside a <pre> block. Nobody reads a page
// whose whole content is a JSON document, and the richer the report got the
// worse that page became — the comparison and the ranked findings this release
// adds would have arrived as more lines of braces.
//
// The page is now a page: what the report says, in tables, with the machine
// readable copy kept in a collapsed section for whoever wants to parse it.

// confluenceLabel is what a figure is called on the page. Anything not named
// here is printed under its own key rather than dropped: a report that grows a
// field should show it, not hide it.
var confluenceLabel = map[string]string{
	"users": "사용자", "new_users": "신규 사용자", "sessions": "세션",
	"page_views": "페이지뷰", "events": "이벤트", "conversions": "전환",
	"conversion_users": "전환 사용자", "conversion_sessions": "전환 세션",
	"conversion_rate": "전환율", "user_conversion_rate": "사용자 전환율",
	"session_conversion_rate": "세션 전환율", "engagement_rate": "참여율",
	"avg_session_duration": "평균 세션 시간(초)", "revenue": "매출",
	"calls": "호출", "success_rate": "성공률", "average_latency_ms": "평균 지연(ms)",
	"input_tokens": "입력 토큰", "output_tokens": "출력 토큰", "cost": "비용",
	"fallbacks": "폴백", "errors": "오류", "affected_users": "영향받은 사용자",
	"error_users": "오류를 겪은 사용자", "error_user_conversion_rate": "오류 사용자 전환율",
	"clean_user_conversion_rate": "무오류 사용자 전환율", "conversion_rate_delta": "전환율 차이",
	"matched_entities": "대상 인원", "segment_name": "Segment",
}

// confluenceOrder keeps the headline figures in a fixed, readable order. The
// rest follow alphabetically so the page does not reshuffle between runs.
var confluenceOrder = []string{"users", "new_users", "sessions", "page_views", "events",
	"conversions", "conversion_users", "conversion_rate", "engagement_rate",
	"avg_session_duration", "revenue"}

func confluenceTitle(key string) string {
	if label, ok := confluenceLabel[key]; ok {
		return label
	}
	return key
}

// confluenceNumber prints a figure the way a reader expects rather than the way
// Go marshals it: no exponent, no trailing zeros, and a thousands separator on
// the counts that get large.
func confluenceNumber(value any) string {
	switch number := value.(type) {
	case int64:
		return groupDigits(fmt.Sprintf("%d", number))
	case float64:
		if number == float64(int64(number)) {
			return groupDigits(fmt.Sprintf("%d", int64(number)))
		}
		return fmt.Sprintf("%.2f", number)
	case string:
		return number
	case bool:
		if number {
			return "예"
		}
		return "아니오"
	case nil:
		return "—"
	default:
		return fmt.Sprint(value)
	}
}

func groupDigits(digits string) string {
	negative := strings.HasPrefix(digits, "-")
	digits = strings.TrimPrefix(digits, "-")
	if len(digits) <= 3 {
		if negative {
			return "-" + digits
		}
		return digits
	}
	var parts []string
	for len(digits) > 3 {
		parts = append([]string{digits[len(digits)-3:]}, parts...)
		digits = digits[:len(digits)-3]
	}
	parts = append([]string{digits}, parts...)
	joined := strings.Join(parts, ",")
	if negative {
		return "-" + joined
	}
	return joined
}

// confluenceChange states a movement with its direction, so a reader does not
// have to work out whether -12% is the good one.
func confluenceChange(value any) string {
	change, ok := value.(float64)
	if !ok {
		return "—"
	}
	switch {
	case change > 0:
		return fmt.Sprintf("▲ %.1f%%", change)
	case change < 0:
		return fmt.Sprintf("▼ %.1f%%", -change)
	default:
		return "변화 없음"
	}
}

func escape(value string) string { return html.EscapeString(value) }

type confluenceBuilder struct{ body strings.Builder }

func (b *confluenceBuilder) heading(level int, text string) {
	fmt.Fprintf(&b.body, "<h%d>%s</h%d>", level, escape(text), level)
}

func (b *confluenceBuilder) paragraph(text string) {
	fmt.Fprintf(&b.body, "<p>%s</p>", escape(text))
}

// table writes a header row and the rows under it. Cells are escaped; nothing
// reaching this function is trusted, because event properties end up in it.
func (b *confluenceBuilder) table(headers []string, rows [][]string) {
	if len(rows) == 0 {
		return
	}
	b.body.WriteString("<table><tbody><tr>")
	for _, header := range headers {
		fmt.Fprintf(&b.body, "<th>%s</th>", escape(header))
	}
	b.body.WriteString("</tr>")
	for _, row := range rows {
		b.body.WriteString("<tr>")
		for _, cell := range row {
			fmt.Fprintf(&b.body, "<td>%s</td>", escape(cell))
		}
		b.body.WriteString("</tr>")
	}
	b.body.WriteString("</tbody></table>")
}

// expand puts the machine readable copy behind a disclosure, so the page reads
// as a report and still carries everything the webhook would have sent.
func (b *confluenceBuilder) expand(title, preformatted string) {
	fmt.Fprintf(&b.body,
		`<ac:structured-macro ac:name="expand"><ac:parameter ac:name="title">%s</ac:parameter><ac:rich-text-body><pre>%s</pre></ac:rich-text-body></ac:structured-macro>`,
		escape(title), escape(preformatted))
}

// orderedKeys returns the headline figures first and the rest in a stable order.
func orderedKeys(values map[string]any) []string {
	seen := map[string]bool{}
	keys := []string{}
	for _, key := range confluenceOrder {
		if _, ok := values[key]; ok {
			keys = append(keys, key)
			seen[key] = true
		}
	}
	rest := []string{}
	for key := range values {
		if !seen[key] {
			rest = append(rest, key)
		}
	}
	sort.Strings(rest)
	return append(keys, rest...)
}

// confluenceBody renders one delivery as a Confluence storage-format page.
func confluenceBody(payload map[string]any) string {
	b := &confluenceBuilder{}
	name, _ := payload["name"].(string)
	kind, _ := payload["kind"].(string)
	b.heading(2, name)

	period := []string{}
	if from, ok := payload["from"].(time.Time); ok {
		if to, ok := payload["to"].(time.Time); ok {
			period = append(period, fmt.Sprintf("%s ~ %s", from.Format("2006-01-02"), to.Add(-time.Nanosecond).Format("2006-01-02")))
		}
	}
	if environment, ok := payload["environment"].(string); ok && environment != "" {
		period = append(period, "환경 "+strings.ToUpper(environment))
	}
	if site, ok := payload["site_id"].(string); ok && site != "" {
		period = append(period, "사이트 "+site)
	}
	if len(period) > 0 {
		b.paragraph(strings.Join(period, " · "))
	}

	data, _ := payload["data"].(map[string]any)
	switch {
	case data == nil:
		b.paragraph("이 배달에는 내용이 없습니다.")
	case data["current"] != nil:
		// The period beside the one before it. A single column of numbers is what
		// this page used to be, and it cannot say whether anything moved.
		current, _ := data["current"].(map[string]any)
		previous, _ := data["previous"].(map[string]any)
		change, _ := data["change_percent"].(map[string]any)
		rows := [][]string{}
		for _, key := range orderedKeys(current) {
			rows = append(rows, []string{confluenceTitle(key), confluenceNumber(current[key]),
				confluenceNumber(previous[key]), confluenceChange(change[key])})
		}
		b.table([]string{"지표", "이번 기간", "이전 기간", "변화"}, rows)
		if insights, ok := data["insights"].([]any); ok && len(insights) > 0 {
			b.heading(3, "주목할 변화")
			found := [][]string{}
			for _, item := range insights {
				insight, _ := item.(map[string]any)
				if insight == nil {
					continue
				}
				found = append(found, []string{
					fmt.Sprint(insight["title"]), fmt.Sprint(insight["severity"]),
					confluenceChange(insight["change_percent"]),
					confluenceNumber(insight["current"]), confluenceNumber(insight["previous"]),
					fmt.Sprint(insight["recommendation"]),
				})
			}
			b.table([]string{"항목", "심각도", "변화", "현재", "이전", "권장 조치"}, found)
		}
	case data["kpis"] != nil:
		// The visitor insight report: a sentence, the KPIs against the previous
		// period, and the ranked findings with the action each one implies. It is
		// the richest report Momento sends and it was arriving as the largest
		// block of JSON.
		if headline, ok := data["headline"].(string); ok && headline != "" {
			b.paragraph(headline)
		}
		kpis, _ := data["kpis"].([]any)
		rows := [][]string{}
		for _, item := range kpis {
			kpi, _ := item.(map[string]any)
			if kpi == nil {
				continue
			}
			rows = append(rows, []string{fmt.Sprint(kpi["label"]), confluenceNumber(kpi["current"]),
				confluenceNumber(kpi["previous"]), confluenceChange(kpi["change_percent"])})
		}
		b.table([]string{"지표", "이번 기간", "이전 기간", "변화"}, rows)
		findings, _ := data["findings"].([]any)
		if len(findings) > 0 {
			b.heading(3, "발견")
			found := [][]string{}
			for _, item := range findings {
				finding, _ := item.(map[string]any)
				if finding == nil {
					continue
				}
				found = append(found, []string{fmt.Sprint(finding["title"]), fmt.Sprint(finding["severity"]),
					fmt.Sprint(finding["evidence"]), fmt.Sprint(finding["action"])})
			}
			b.table([]string{"제목", "심각도", "근거", "조치"}, found)
		}
	case data["rows"] != nil:
		// The AI report and anything else that answers a list of labelled rows.
		rows, _ := data["rows"].([]any)
		if len(rows) == 0 {
			b.paragraph("이 기간에는 기록된 항목이 없습니다.")
			break
		}
		first, _ := rows[0].(map[string]any)
		keys := orderedKeys(first)
		headers := []string{"항목"}
		for _, key := range keys {
			if key == "label" {
				continue
			}
			headers = append(headers, confluenceTitle(key))
		}
		table := [][]string{}
		for _, item := range rows {
			row, _ := item.(map[string]any)
			if row == nil {
				continue
			}
			cells := []string{fmt.Sprint(row["label"])}
			for _, key := range keys {
				if key == "label" {
					continue
				}
				cells = append(cells, confluenceNumber(row[key]))
			}
			table = append(table, cells)
		}
		b.table(headers, table)
		if totals, ok := data["totals"].(map[string]any); ok {
			b.heading(3, "합계")
			summary := [][]string{}
			for _, key := range orderedKeys(totals) {
				summary = append(summary, []string{confluenceTitle(key), confluenceNumber(totals[key])})
			}
			b.table([]string{"지표", "값"}, summary)
		}
	default:
		// Every other report: its own figures, one per row. Lists and objects
		// inside it stay in the attached document rather than being flattened
		// into something that reads like a number.
		rows := [][]string{}
		for _, key := range orderedKeys(data) {
			switch data[key].(type) {
			case []any, map[string]any:
				continue
			}
			rows = append(rows, []string{confluenceTitle(key), confluenceNumber(data[key])})
		}
		if len(rows) == 0 {
			b.paragraph("이 리포트의 내용은 아래 원본에 있습니다.")
		}
		b.table([]string{"지표", "값"}, rows)
	}

	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		// The page still has to be publishable: a report that cannot be attached
		// is better than no page at all, and the reader is told which it is.
		b.paragraph("원본 문서를 첨부하지 못했습니다: " + err.Error())
	} else {
		b.expand("원본 데이터 ("+kind+")", string(raw))
	}
	return b.body.String()
}
