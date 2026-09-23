package store

import "strings"

/*
Optional fields on an upsert: what "absent" means, and why it is not "blank".

Every upsert in this package is reached by `POST /<collection>` with the
business id in the body — there is no PATCH route anywhere. That made the
distinction between "the caller did not mention this column" and "the caller
wants this column empty" invisible: both arrived as the zero value, and
`ON CONFLICT ... DO UPDATE SET col = EXCLUDED.col` wrote the empty string over
whatever was stored. A form that owns six of an instrument's eleven columns
therefore erased the other five every time somebody saved it.

The contract these helpers exist to express:

	nil   the caller did not send the field   -> keep what is stored
	""    the caller sent it empty            -> clear the column
	value                                     -> set the column

Three rules follow, and all three are easy to get subtly wrong:

  - Guard against the PARAMETER, not against EXCLUDED, whenever the VALUES
    clause coalesced the NULL away — which it must here, because these columns
    are NOT NULL DEFAULT ''. `EXCLUDED.col` is then '' rather than NULL, so
    `COALESCE(EXCLUDED.col, t.col)` compiles, runs, and does nothing at all. It
    must be `COALESCE($n, t.col)`.

    Guarding against EXCLUDED is right where the column is nullable and the
    parameter reaches VALUES untouched — see UpsertBatchLabSummary in
    lab_summary.go, which is correct as written. The difference is whether a
    NULL survives the VALUES clause.

  - Cast the parameter explicitly ($n::text, $n::int). A parameter used only
    inside COALESCE next to a column has no other type to be inferred from,
    and pgx will not guess.

  - A nullable DATE needs three branches rather than a COALESCE, because its
    "cleared" state is NULL rather than a zero value:
    CASE WHEN $n::text IS NULL THEN t.col WHEN $n::text = '' THEN NULL
    ELSE $n::date END, with NULLIF($n::text,'')::date on the VALUES side.
*/

// trimOptional trims an optional string while preserving the nil/value
// distinction that COALESCE depends on.
func trimOptional(v *string) *string {
	if v == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*v)
	return &trimmed
}

// blankToNil folds an empty optional string back into "not sent".
//
// For columns where an empty value is meaningless rather than a deliberate
// clear — a status, a name — so that a form posting "" keeps the stored value
// instead of writing a row nothing can interpret.
func blankToNil(v *string) *string {
	t := trimOptional(v)
	if t == nil || *t == "" {
		return nil
	}
	return t
}
