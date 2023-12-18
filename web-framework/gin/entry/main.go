package main

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

type Account struct {
	Id   int    `json:"id" binding:"required"`
	Name string `json:"name" binding:"required"`
}

func newServer() *gin.Engine {
	r := gin.Default()
	r.GET("", helloHandler)
	r.GET("/:name", helloUserHandler)
	r.POST("/add", helloAccountHandler)

	return r
}

func helloHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"resp": "response without parameter",
	})
}

func helloUserHandler(c *gin.Context) {
	name := c.Param("name")
	c.JSON(http.StatusOK, gin.H{
		"resp": fmt.Sprintf("response with parameter: %v", name),
	})
}

func helloAccountHandler(c *gin.Context) {
	var data Account
	if err := c.ShouldBind(&data); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("err: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"received": data,
	})
}

func main() {
	newServer().Run()
}
