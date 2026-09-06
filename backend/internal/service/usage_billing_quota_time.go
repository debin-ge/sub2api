package service

import (
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

const videoQuotaTimeContractVersion = 1

var ErrUsageBillingQuotaCalendarMismatch = errors.New("video quota calendar differs from the frozen contract")

type UsageBillingQuotaTime struct {
	Version   int       `json:"version"`
	TimeZone  string    `json:"time_zone"`
	DayStart  time.Time `json:"day_start"`
	WeekStart time.Time `json:"week_start"`
}

func (c *UsageBillingCommand) ValidateQuotaTime() error {
	if c == nil || c.QuotaTime == nil {
		return nil
	}
	clock := c.QuotaTime
	if clock.Version != videoQuotaTimeContractVersion || c.MediaType != "video" ||
		c.OccurredAt.IsZero() || c.OccurredAt.Before(time.Unix(0, 0)) ||
		clock.DayStart.IsZero() || clock.WeekStart.IsZero() ||
		clock.DayStart.After(c.OccurredAt) || c.OccurredAt.Sub(clock.DayStart) >= 25*time.Hour ||
		clock.WeekStart.After(clock.DayStart) || clock.DayStart.Sub(clock.WeekStart) >= 7*24*time.Hour {
		return fmt.Errorf("%w: video quota time contract is invalid", ErrUsageBillingPayloadInvalid)
	}
	expected, err := ResolveUsageBillingQuotaTime(c.OccurredAt, clock.TimeZone)
	if err != nil || !expected.DayStart.Equal(clock.DayStart) || !expected.WeekStart.Equal(clock.WeekStart) {
		return fmt.Errorf("%w: video quota calendar is invalid", ErrUsageBillingPayloadInvalid)
	}
	return nil
}

func videoQuotaTimeZone() string {
	name := timezone.Location().String()
	if name == "" || name == "Local" {
		return "UTC"
	}
	return name
}

func videoUsageQuotaTime(task *VideoTask) (*UsageBillingQuotaTime, error) {
	if _, exists := task.PriceSnapshot["quota_time_contract_version"]; !exists {
		return nil, nil
	}
	version, valid := numericMapValue(task.PriceSnapshot, "quota_time_contract_version")
	zone, _ := task.PriceSnapshot["quota_time_zone"].(string)
	if !valid || version != videoQuotaTimeContractVersion || zone == "" || zone == "Local" ||
		task.FinishedAt == nil || task.FinishedAt.IsZero() || task.FinishedAt.Before(task.CreatedAt) {
		return nil, fmt.Errorf("%w: video quota time snapshot is incomplete", ErrUsageBillingPayloadInvalid)
	}
	clock, err := ResolveUsageBillingQuotaTime(*task.FinishedAt, zone)
	if err != nil {
		return nil, err
	}
	command := &UsageBillingCommand{MediaType: "video", OccurredAt: *task.FinishedAt, QuotaTime: clock}
	if err := command.ValidateQuotaTime(); err != nil {
		return nil, err
	}
	return clock, nil
}

func ResolveUsageBillingQuotaTime(occurredAt time.Time, zone string) (*UsageBillingQuotaTime, error) {
	if zone == "" || zone == "Local" || occurredAt.IsZero() {
		return nil, fmt.Errorf("%w: video quota calendar is missing", ErrUsageBillingPayloadInvalid)
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		return nil, fmt.Errorf("%w: video quota time zone is invalid", ErrUsageBillingPayloadInvalid)
	}
	occurredAt = occurredAt.In(location)
	dayStart := time.Date(occurredAt.Year(), occurredAt.Month(), occurredAt.Day(), 0, 0, 0, 0, location)
	weekday := (int(occurredAt.Weekday()) + 6) % 7
	return &UsageBillingQuotaTime{
		Version: videoQuotaTimeContractVersion, TimeZone: zone, DayStart: dayStart.UTC(),
		WeekStart: time.Date(occurredAt.Year(), occurredAt.Month(), occurredAt.Day()-weekday, 0, 0, 0, 0, location).UTC(),
	}, nil
}
