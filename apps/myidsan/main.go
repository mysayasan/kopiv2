package main

import (
	myidsanapp "github.com/mysayasan/kopiv2/apps/myidsan/app"
	"github.com/mysayasan/kopiv2/infra/apphost"
)

func main() {
	if err := apphost.Run(myidsanapp.New()); err != nil {
		panic(err)
	}
}
