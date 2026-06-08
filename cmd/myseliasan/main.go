package main

import (
	myseliasanapp "github.com/mysayasan/kopiv2/apps/myseliasan/app"
	"github.com/mysayasan/kopiv2/infra/apphost"
)

func main() {
	if err := apphost.Run(myseliasanapp.New()); err != nil {
		panic(err)
	}
}
