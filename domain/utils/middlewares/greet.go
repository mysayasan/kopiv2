package middlewares

import "github.com/gofiber/fiber/v2"

// GreetMiddleware struct
type GreetMiddleware struct {
}

// Init
func NewGreet() *GreetMiddleware {
	return &GreetMiddleware{}
}

// Greet
func (m *GreetMiddleware) Greet(c *fiber.Ctx) error {
	c.Set("Server", "r450k")

	// Go to next middleware:
	return c.Next()
}
