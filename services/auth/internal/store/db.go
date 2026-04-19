package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type User struct {
	ID           string
	Email        string
	Market       string
	PasswordHash string
}

func ConnectPG(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("pg: connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("pg: ping: %w", err)
	}
	return pool, nil
}

func GetUserByEmail(ctx context.Context, db *pgxpool.Pool, email string) (*User, error) {
	u := &User{}
	err := db.QueryRow(ctx,
		`SELECT id::text, email, COALESCE(market, ''), password_hash
		 FROM users WHERE email = $1`, email,
	).Scan(&u.ID, &u.Email, &u.Market, &u.PasswordHash)
	if err != nil {
		return nil, fmt.Errorf("pg: get user: %w", err)
	}
	return u, nil
}

// LoadCasbinRules returns all rows from casbin_rule as [ptype, v0, v1, v2, ...].
func LoadCasbinRules(ctx context.Context, db *pgxpool.Pool) ([][]string, error) {
	rows, err := db.Query(ctx,
		`SELECT ptype,
			COALESCE(v0,''), COALESCE(v1,''), COALESCE(v2,''),
			COALESCE(v3,''), COALESCE(v4,''), COALESCE(v5,'')
		 FROM casbin_rule`)
	if err != nil {
		return nil, fmt.Errorf("pg: load casbin rules: %w", err)
	}
	defer rows.Close()

	var rules [][]string
	for rows.Next() {
		var ptype, v0, v1, v2, v3, v4, v5 string
		if err := rows.Scan(&ptype, &v0, &v1, &v2, &v3, &v4, &v5); err != nil {
			return nil, err
		}
		rule := []string{ptype, v0, v1, v2, v3, v4, v5}
		// Trim empty trailing values
		for len(rule) > 1 && rule[len(rule)-1] == "" {
			rule = rule[:len(rule)-1]
		}
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}
