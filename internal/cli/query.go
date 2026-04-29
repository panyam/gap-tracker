package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/panyam/gap-tracker/internal/render"
)

// newQueryCmd is `gapt query <project> <sql>` — raw SQL escape hatch.
//
// Read-only by default (PRAGMA query_only = ON). Useful for ad-hoc
// exploration and scripting against the gap-tracker DB without having
// to reach for sqlite3 directly.
func newQueryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "query <project> <sql...>",
		Short: "Run a read-only SQL query against the project DB",
		Long: `Execute arbitrary read-only SQL and render the results as a table.

Examples:
  gapt query mcpkit "SELECT repo, count(*) FROM issues GROUP BY repo"
  gapt query mcpkit "SELECT number, title FROM issues WHERE labels LIKE '%bug%' LIMIT 10"
  gapt query mcpkit "SELECT bucket, count(*) AS n FROM triage GROUP BY bucket ORDER BY n DESC"`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, rest, err := loadAppFromArgs(args)
			if err != nil {
				return err
			}
			defer a.Close()

			sql := strings.Join(rest, " ")

			// Enforce read-only so we can't accidentally mutate the DB.
			if err := a.Store.DB().Exec("PRAGMA query_only = ON").Error; err != nil {
				return fmt.Errorf("set query_only: %w", err)
			}

			sqlDB, err := a.Store.DB().DB()
			if err != nil {
				return fmt.Errorf("get sql.DB: %w", err)
			}

			rows, err := sqlDB.Query(sql)
			if err != nil {
				return fmt.Errorf("query: %w", err)
			}
			defer rows.Close()

			cols, err := rows.Columns()
			if err != nil {
				return fmt.Errorf("columns: %w", err)
			}

			var resultRows [][]string
			for rows.Next() {
				// Scan all columns as *string to handle any type.
				vals := make([]any, len(cols))
				ptrs := make([]any, len(cols))
				for i := range vals {
					ptrs[i] = &vals[i]
				}
				if err := rows.Scan(ptrs...); err != nil {
					return fmt.Errorf("scan: %w", err)
				}
				row := make([]string, len(cols))
				for i, v := range vals {
					if v == nil {
						row[i] = "NULL"
					} else {
						row[i] = fmt.Sprintf("%v", v)
					}
				}
				resultRows = append(resultRows, row)
			}
			if err := rows.Err(); err != nil {
				return fmt.Errorf("rows: %w", err)
			}

			return a.Renderer.RenderQuery(os.Stdout, render.QueryReport{
				SQL:     sql,
				Columns: cols,
				Rows:    resultRows,
			})
		},
	}
}
