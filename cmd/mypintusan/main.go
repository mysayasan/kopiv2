package main

import (
	mypintusanapp "github.com/mysayasan/kopiv2/apps/mypintusan/app"
	"github.com/mysayasan/kopiv2/infra/apphost"
)

func main() {
	if err := apphost.Run(mypintusanapp.New()); err != nil {
		panic(err)
	}
}
