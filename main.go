package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	myidsanapp "github.com/mysayasan/kopiv2/apps/myidsan/app"
	mymatasanapp "github.com/mysayasan/kopiv2/apps/mymatasan/app"
	myseliasanapp "github.com/mysayasan/kopiv2/apps/myseliasan/app"
	"github.com/mysayasan/kopiv2/infra/apphost"
)

func main() {
	selected := flag.String("app", "mymatasan", "app module to run")
	flag.Parse()

	apps := map[string]apphost.App{
		"myidsan":    myidsanapp.New(),
		"mymatasan":  mymatasanapp.New(),
		"myseliasan": myseliasanapp.New(),
	}

	appName := strings.TrimSpace(*selected)
	app, ok := apps[appName]
	if !ok {
		available := make([]string, 0, len(apps))
		for name := range apps {
			available = append(available, name)
		}
		sort.Strings(available)
		fmt.Fprintf(os.Stderr, "unknown app %q. available apps: %s\n", appName, strings.Join(available, ", "))
		os.Exit(2)
	}

	if err := apphost.Run(app); err != nil {
		panic(err)
	}
}
