package main

import (
	"fmt"
	"net/http"
)

func main() {
	_, err := http.Get("http://:8081/")
	fmt.Println(err)
}
