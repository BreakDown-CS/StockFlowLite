package response

import (
	"github.com/gofiber/fiber/v2"
)

// Envelope represents the standardized API JSON response structure.
type Envelope struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
	Meta    interface{} `json:"meta,omitempty"`
	Error   interface{} `json:"error,omitempty"`
}

// ErrorDetail describes an error for client consumption without leaking internals.
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// PageMeta provides pagination context for list responses.
type PageMeta struct {
	Limit  int   `json:"limit"`
	Offset int   `json:"offset"`
	Total  int64 `json:"total"`
}

// OK sends an HTTP 200 response with data and an optional message.
func OK(c *fiber.Ctx, data interface{}, message ...string) error {
	msg := ""
	if len(message) > 0 {
		msg = message[0]
	}
	return c.Status(fiber.StatusOK).JSON(Envelope{
		Success: true,
		Message: msg,
		Data:    data,
	})
}

// Created sends an HTTP 201 response with data and an optional message.
func Created(c *fiber.Ctx, data interface{}, message ...string) error {
	msg := ""
	if len(message) > 0 {
		msg = message[0]
	}
	return c.Status(fiber.StatusCreated).JSON(Envelope{
		Success: true,
		Message: msg,
		Data:    data,
	})
}

// SuccessWithMeta sends an HTTP 200 response with data, pagination metadata, and an optional message.
func SuccessWithMeta(c *fiber.Ctx, data interface{}, meta interface{}, message ...string) error {
	msg := ""
	if len(message) > 0 {
		msg = message[0]
	}
	return c.Status(fiber.StatusOK).JSON(Envelope{
		Success: true,
		Message: msg,
		Data:    data,
		Meta:    meta,
	})
}

// Error sends a failure response with an appropriate HTTP status code.
func Error(c *fiber.Ctx, status int, code, message string) error {
	return c.Status(status).JSON(Envelope{
		Success: false,
		Error: ErrorDetail{
			Code:    code,
			Message: message,
		},
	})
}
