package catalog

import (
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestParseMaterialListQuerySupportsFrontendAliases(t *testing.T) {
	values := url.Values{
		"cari":          {"Newton"},
		"jenjang":       {"sma"},
		"kelas":         {"11"},
		"mataPelajaran": {"Fisika"},
		"akses":         {"gratis"},
		"urutan":        {"latihan"},
		"halaman":       {"3"},
		"batas":         {"12"},
		"isLanding":     {"true"},
	}
	query, err := parseListQuery(values, false)
	if err != nil {
		t.Fatalf("parseListQuery() error = %v", err)
	}
	if query.Search != "Newton" || query.Level != "SMA" || query.Grade != 11 ||
		query.Subject != "Fisika" || query.Access != "FREE" || query.Sort != "latihan" ||
		query.Page != 3 || query.PageSize != 12 || query.Offset != 24 || !query.Landing {
		t.Fatalf("parsed query = %+v", query)
	}
}

func TestParsePackageQueryValidatesEnumsAndPageBounds(t *testing.T) {
	query, err := parseListQuery(url.Values{
		"kind":       {"simulation"},
		"difficulty": {"sulit"},
		"sort":       {"soal"},
	}, true)
	if err != nil {
		t.Fatalf("parseListQuery() error = %v", err)
	}
	if query.Kind != "SIMULATION" || query.Difficulty != "HARD" || query.Sort != "soal" {
		t.Fatalf("parsed package query = %+v", query)
	}

	for _, invalid := range []url.Values{
		{"pageSize": {"101"}},
		{"grade": {"13"}},
		{"accessTier": {"MEMBER"}},
		{"kind": {"EXAM"}},
		{"difficulty": {"EXTREME"}},
	} {
		if _, err := parseListQuery(invalid, true); err == nil {
			t.Errorf("parseListQuery(%v) error = nil, want validation error", invalid)
		}
	}
}

func TestMaterialWhereUsesBoundParameters(t *testing.T) {
	search := "x' OR 1=1 --"
	query := listQuery{
		Search: search, Level: "SMA", Grade: 11,
		Subject: "Fisika", Access: "FREE",
	}
	where, args := materialWhere(query)
	if strings.Contains(where, search) {
		t.Fatal("search text was interpolated into SQL")
	}
	if strings.Contains(where, "?") || !strings.Contains(where, "m.title ILIKE $1") ||
		!strings.Contains(where, "m.access_tier = $9") {
		t.Fatalf("where = %q, want PostgreSQL placeholders and case-insensitive search", where)
	}
	want := []any{"%" + search + "%", "%" + search + "%", "%" + search + "%", "%" + search + "%", "SMA", 11, "Fisika", "Fisika", "FREE"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
}

func TestMaterialWhereFiltersLandingMaterials(t *testing.T) {
	where, args := materialWhere(listQuery{Landing: true})
	if !strings.Contains(where, "m.is_landing = TRUE") {
		t.Fatalf("where = %q, want is_landing filter", where)
	}
	if len(args) != 0 {
		t.Fatalf("args = %#v, want no arguments", args)
	}
}

func TestMaterialRelevanceSortHasStableTieBreakers(t *testing.T) {
	order, args := materialOrder(listQuery{Search: "newton", Sort: "relevan"}, 10)
	if !strings.Contains(order, "CASE WHEN") || !strings.Contains(order, "m.id ASC") ||
		!strings.Contains(order, "LOWER($10)") || !strings.Contains(order, "m.title ILIKE $11") ||
		!strings.Contains(order, "s.name ILIKE $12") {
		t.Fatalf("order = %q, want relevance and stable tie-breakers", order)
	}
	if !reflect.DeepEqual(args, []any{"newton", "%newton%", "%newton%"}) {
		t.Fatalf("order args = %#v", args)
	}
}

func TestPackageWhereUsesSequentialPostgresParameters(t *testing.T) {
	where, args := packageWhere(listQuery{Search: "bio", Kind: "PRACTICE", Level: "SMA", Grade: 10})
	if strings.Contains(where, "?") ||
		!strings.Contains(where, "p.title ILIKE $1") ||
		!strings.Contains(where, "p.kind = $4") ||
		!strings.Contains(where, "p.grade = $6") {
		t.Fatalf("where = %q, want sequential PostgreSQL placeholders", where)
	}
	if !reflect.DeepEqual(args, []any{"%bio%", "%bio%", "%bio%", "PRACTICE", "SMA", 10}) {
		t.Fatalf("args = %#v", args)
	}
}
