package insight

// RevenueAmountSQL is the one definition of what a purchase was worth.
//
// The tracker lets a purchase carry its amount as either `value` or `revenue`,
// the amount has to be checked before it is cast because a site can send
// anything, and a refund is negative — three decisions that have to be made the
// same way everywhere or two screens report different money for the same day.
//
// They were written out by hand in seven places across two packages: the
// overview, the query builder, the ecommerce report, the platform rollup, the
// MCP tool, the scheduled digest, and the daily rollups the collector fills.
// All seven agreed, which is not the same as being unable to disagree — the
// digest and the overview are computed in different packages and a reader
// comparing them has no way to tell a definition change from a data change.
//
// alias is the table alias the expression reads through, empty for none.
func RevenueAmountSQL(alias string) string {
	prefix := ""
	if alias != "" {
		prefix = alias + "."
	}
	amount := "coalesce(" + prefix + "properties->>'value'," + prefix + "properties->>'revenue')"
	return "coalesce(sum(CASE WHEN " + prefix + "event_name='purchase' AND coalesce(" + amount +
		",'') ~ '^-?[0-9]+(\\.[0-9]+)?$' THEN " + amount + "::numeric ELSE 0 END),0)"
}
