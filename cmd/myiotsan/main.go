package main

import (
	myiotsanapp "github.com/mysayasan/kopiv2/apps/myiotsan/app"
	"github.com/mysayasan/kopiv2/infra/apphost"
)

func main() {
	if err := apphost.Run(myiotsanapp.New()); err != nil {
		panic(err)
	}
}
