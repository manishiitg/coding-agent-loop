package services

import (
	"context"
	"testing"
)

func TestWhatsAppUserNotificationSkipsWhenUnpaired(t *testing.T) {
	svc := &WhatsAppService{}

	msgID, err := svc.SendUserNotification(context.Background(), "hello", "", &NotificationDestination{UserID: "user-1"})
	if err != nil {
		t.Fatalf("SendUserNotification returned error for unpaired WhatsApp service: %v", err)
	}
	if msgID != "" {
		t.Fatalf("SendUserNotification msgID = %q, want empty", msgID)
	}
}
