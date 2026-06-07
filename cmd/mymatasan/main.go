package main

import (
	mymatasanapp "github.com/mysayasan/kopiv2/apps/mymatasan/app"
	"github.com/mysayasan/kopiv2/infra/apphost"
)

func main() {
	if err := apphost.Run(mymatasanapp.New()); err != nil {
		panic(err)
	}
}
