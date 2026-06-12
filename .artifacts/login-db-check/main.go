package main

import (
	"database/sql"
	"fmt"
	_ "github.com/lib/pq"
)

func check(port int) {
	dsn := fmt.Sprintf("host=localhost port=%d user=postgres password=postgres dbname=mymatasandb sslmode=disable", port)
	db, err := sql.Open("postgres", dsn)
	if err != nil { fmt.Printf("port=%d open err=%v\n", port, err); return }
	defer db.Close()
	if err := db.Ping(); err != nil { fmt.Printf("port=%d ping err=%v\n", port, err); return }
	var id int64
	var email, pwd string
	var role int64
	var active bool
	err = db.QueryRow("SELECT id, email, userpwd, user_role_id, is_active FROM user_login WHERE email = $1", "superadmin").Scan(&id, &email, &pwd, &role, &active)
	if err != nil { fmt.Printf("port=%d query err=%v\n", port, err); return }
	fmt.Printf("port=%d id=%d email=%s pwd=%q role=%d active=%t\n", port, id, email, pwd, role, active)
}

func main() { check(5433); check(5432) }
