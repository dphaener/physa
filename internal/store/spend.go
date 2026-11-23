package store

import "github.com/dphaener/physa/internal/spend"

// Spent is what one init phase spent, as the run's meter counted it: its
// raw tokens, and its weighted tokens with their cost estimate.
type Spent struct {
	Phase string
	spend.Count
}

// AddSpent adds p to what its phase has spent so far.
func (s *Store) AddSpent(p Spent) error {
	_, err := s.db.Exec(`INSERT INTO init_spend (phase, input, cached_input, output, weighted, micro_usd, unpriced) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (phase) DO UPDATE SET input = input + excluded.input, cached_input = cached_input + excluded.cached_input,
		output = output + excluded.output, weighted = weighted + excluded.weighted, micro_usd = micro_usd + excluded.micro_usd,
		unpriced = max(unpriced, excluded.unpriced)`,
		p.Phase, p.Raw.Input, p.Raw.CachedInput, p.Raw.Output, p.Spend.Tokens, p.Spend.MicroUSD, p.Spend.Unpriced)
	return err
}

// Spending returns what each init phase has spent, in run order: discover,
// propose, recover.
func (s *Store) Spending() ([]Spent, error) {
	rows, err := s.db.Query(`SELECT phase, input, cached_input, output, weighted, micro_usd, unpriced FROM init_spend
		ORDER BY CASE phase WHEN 'discover' THEN 0 WHEN 'propose' THEN 1 WHEN 'recover' THEN 2 ELSE 3 END, phase`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Spent
	for rows.Next() {
		var p Spent
		if err := rows.Scan(&p.Phase, &p.Raw.Input, &p.Raw.CachedInput, &p.Raw.Output, &p.Spend.Tokens, &p.Spend.MicroUSD, &p.Spend.Unpriced); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
