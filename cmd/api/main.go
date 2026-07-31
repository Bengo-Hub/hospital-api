package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/bengobox/hospital-service/internal/app"
)

// @title Hospital Service API (Codevertex Afya)
// @version 0.1.0
// @description HTTP API for the Codevertex Afya hospital management service. Consultation, laboratory, pharmacy, inpatient, and billing/insurance integration.
// @BasePath /api/v1
// @schemes http https
// @securityDefinitions.apikey bearerAuth
// @in header
// @name Authorization
// @description JWT token from auth-service. Format: Bearer {token}
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	a, err := app.New(ctx)
	if err != nil {
		log.Fatalf("failed to initialise app: %v", err)
	}
	defer a.Close()

	if err := a.Run(ctx); err != nil {
		log.Fatalf("runtime error: %v", err)
	}
}
