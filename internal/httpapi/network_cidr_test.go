package httpapi

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 망 구분 추가 폼은 `10.20.30.1/24` 를 막지 않는다 — web/src/pages/cidrRule.ts 가
// 호스트 비트를 거절하지 않고 「대역의 시작 주소는 10.20.30.0/24 입니다.」 라는
// 안내만 보여 주기로 의도적으로 정해 두었기 때문이다. 그런데 서버는 net.ParseCIDR
// 이 돌려준 대역을 버리고 사용자가 적은 글자를 그대로 `cidr cidr NOT NULL`
// 컬럼(001_initial.sql:67)에 넣었다. PostgreSQL 의 cidr 타입은 넷마스크 오른쪽에
// 비트가 켜진 값을 거절하므로(SQLSTATE 22P02) 사용자는 화면이 괜찮다고 한 값으로
// 500 NETWORK_CREATE_FAILED 와 pgx 원문을 받았다.
//
// 화면이 서버의 거울이라는 이 저장소의 계약을 지키는 쪽은 400 으로 거절하는 것이
// 아니라 사용자가 말한 뜻 그대로 — "10.20.30.1 이 속한 /24" — 저장하는 것이다.

func TestNormalizeCIDRAgreesWithParseCIDR(t *testing.T) {
	for _, text := range []string{
		"10.20.30.1/24",
		"10.20.30.0/24",
		"0.0.0.0/0",
		"10.20.30.1/32",
		"192.0.2.0/24",
		"172.16.255.255/12",
		"2001:db8::1/64",
		"2001:db8::/64",
		"::/0",
		"2001:db8::1/128",
	} {
		_, network, err := net.ParseCIDR(text)
		if err != nil {
			t.Fatalf("이 표의 값은 net.ParseCIDR 가 받아들여야 한다: %q: %v", text, err)
		}
		got, err := normalizeCIDR(text)
		if err != nil {
			t.Errorf("%q: 정규화가 거절했다: %v", text, err)
			continue
		}
		if got != network.String() {
			t.Errorf("%q: 정규화 결과가 net.ParseCIDR 의 대역과 다르다: got %q, want %q", text, got, network.String())
		}
	}
}

func TestNormalizeCIDRRejectsWhatParseCIDRRejects(t *testing.T) {
	for _, text := range []string{
		"",
		"10.20.30.0",
		"10.20.30.0/33",
		"10.20.30.0/-1",
		"사내망",
		"2001:db8::1/129",
	} {
		if _, _, err := net.ParseCIDR(text); err == nil {
			t.Fatalf("이 표의 값은 net.ParseCIDR 가 거절해야 한다: %q", text)
		}
		if got, err := normalizeCIDR(text); err == nil {
			t.Errorf("%q: 정규화가 거절하지 않고 %q 를 돌려줬다", text, got)
		}
	}
}

// 결함이 실재했다는 증거: 진짜 PostgreSQL 에 원문을 그대로 넣으면 막히고,
// 정규화한 값은 들어간다. DSN 이 없으면 testPool 이 스스로 skip 한다.
func TestPostgresRefusesACIDRWithHostBitsAndAcceptsTheNormalizedOne(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM network_ranges WHERE name='정규화 단위 확인'`)
	})

	_, err := pool.Exec(ctx, `INSERT INTO network_ranges(name,cidr,description,internal) VALUES('정규화 단위 확인',$1,'',true)`, "10.20.30.1/24")
	if err == nil {
		t.Fatalf("원문 10.20.30.1/24 의 INSERT 가 성공했다 — 이 과제의 전제가 거짓이다")
	}
	if !strings.Contains(err.Error(), "invalid cidr value") {
		t.Fatalf("기대한 cidr 거절이 아닌 다른 실패다: %v", err)
	}

	normalized, err := normalizeCIDR("10.20.30.1/24")
	if err != nil {
		t.Fatalf("정규화가 거절했다: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO network_ranges(name,cidr,description,internal) VALUES('정규화 단위 확인',$1,'',true)`, normalized); err != nil {
		t.Fatalf("정규화한 %q 의 INSERT 가 실패했다: %v", normalized, err)
	}
}

func TestCreateNetworkStoresTheNetworkAddressOfACIDRWithHostBits(t *testing.T) {
	pool := testPool(t)
	server := &Server{DB: pool}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM network_ranges WHERE name LIKE '정규화 핸들러 확인%'`)
	})

	for _, c := range []struct {
		name  string
		given string
		want  string
	}{
		{"정규화 핸들러 확인 호스트비트", "10.20.30.1/24", "10.20.30.0/24"},
		{"정규화 핸들러 확인 이미정규화", "10.20.31.0/24", "10.20.31.0/24"},
		{"정규화 핸들러 확인 전체", "0.0.0.0/0", "0.0.0.0/0"},
		{"정규화 핸들러 확인 단일주소", "10.20.32.1/32", "10.20.32.1/32"},
		{"정규화 핸들러 확인 IPv6", "2001:db8::1/64", "2001:db8::/64"},
	} {
		body := `{"name":"` + c.name + `","cidr":"` + c.given + `","description":"","internal":true}`
		request := httptest.NewRequest(http.MethodPost, "/api/v1/networks", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		server.createNetwork(recorder, request)
		if recorder.Code != 201 {
			t.Errorf("%q: 201 이 아니라 %d 로 돌아왔다: %s", c.given, recorder.Code, recorder.Body.String())
			continue
		}
		for _, leak := range []string{"SQLSTATE", "invalid cidr value", "NETWORK_CREATE_FAILED"} {
			if strings.Contains(recorder.Body.String(), leak) {
				t.Errorf("%q: 응답에 %q 가 샜다: %s", c.given, leak, recorder.Body.String())
			}
		}
	}

	// 저장만 보지 않고 목록이 돌려주는 값까지 읽는다.
	recorder := httptest.NewRecorder()
	server.listNetworks(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/networks", nil))
	if recorder.Code != 200 {
		t.Fatalf("목록이 200 이 아니라 %d 로 돌아왔다: %s", recorder.Code, recorder.Body.String())
	}
	var listed []struct {
		Name string `json:"name"`
		CIDR string `json:"cidr"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &listed); err != nil {
		t.Fatalf("목록을 읽지 못했다: %v", err)
	}
	stored := map[string]string{}
	for _, row := range listed {
		stored[row.Name] = row.CIDR
	}
	for name, want := range map[string]string{
		"정규화 핸들러 확인 호스트비트": "10.20.30.0/24",
		"정규화 핸들러 확인 이미정규화": "10.20.31.0/24",
		"정규화 핸들러 확인 전체":    "0.0.0.0/0",
		"정규화 핸들러 확인 단일주소":  "10.20.32.1/32",
		"정규화 핸들러 확인 IPv6":  "2001:db8::/64",
	} {
		if got := stored[name]; got != want {
			t.Errorf("%s: 목록의 cidr 이 %q 다 — %q 를 기대했다", name, got, want)
		}
	}
}

func TestCreateNetworkStillRejectsWhatParseCIDRRejects(t *testing.T) {
	pool := testPool(t)
	server := &Server{DB: pool}
	for _, given := range []string{"", "10.20.30.0", "10.20.30.0/33"} {
		body, err := json.Marshal(map[string]any{"name": "정규화 거절 확인", "cidr": given, "internal": true})
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "/api/v1/networks", strings.NewReader(string(body)))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		server.createNetwork(recorder, request)
		if recorder.Code != 400 {
			t.Errorf("%q: 400 이 아니라 %d 로 돌아왔다: %s", given, recorder.Code, recorder.Body.String())
			continue
		}
		var out struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &out); err != nil {
			t.Fatalf("%q: 응답을 읽지 못했다: %v (%s)", given, err, recorder.Body.String())
		}
		if out.Error.Code != "INVALID_CIDR" || out.Error.Message != "CIDR is invalid" {
			t.Errorf("%q: 거절 코드·메시지가 바뀌었다: %+v", given, out.Error)
		}
	}
}
