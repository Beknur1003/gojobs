package extract

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Beknur1003/gojobs/internal/models"
)

func TestSalary_Formats_Parsed(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		min, max int
		currency string
		period   models.Period
	}{
		{"rub range with thousands", "Зарплата: от 250 000 до 350 000 ₽", 250000, 350000, "RUB", models.PeriodMonth},
		{"rub k suffix once", "Вилка 250-350к на руки", 250000, 350000, "RUB", models.PeriodMonth},
		{"rub thousand word", "ЗП: 300-400 тыс. руб.", 300000, 400000, "RUB", models.PeriodMonth},
		{"usd prefix range", "💰 $4000-6000", 4000, 6000, "USD", models.PeriodMonth},
		{"usd suffix single from", "Зарплата: от 3500$ net", 3500, 0, "USD", models.PeriodMonth},
		{"eur code with commas", "SALARY: EUR 4,000 - 5,000 (net)", 4000, 5000, "EUR", models.PeriodMonth},
		{"usd k annual", "Salary: $120k - $150k", 120000, 150000, "USD", models.PeriodYear},
		{"explicit year", "€60k-80k per year", 60000, 80000, "EUR", models.PeriodYear},
		{"hourly", "Rate: $90 - $150 /hour", 90, 150, "USD", models.PeriodHour},
		{"up to", "до 400 000 руб", 0, 400000, "RUB", models.PeriodMonth},
		{"kzt million", "Оклад 1.5-2 млн тенге", 1500000, 2000000, "KZT", models.PeriodMonth},
		{"usdt", "Оплата 3000-4000 USDT", 3000, 4000, "USDT", models.PeriodMonth},
		{"multiline picks pay line", "Опыт от 3 лет\nЗарплата 5000-7000 $", 5000, 7000, "USD", models.PeriodMonth},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Salary(tt.in)
			assert.Equal(t, tt.min, s.Min, "min")
			assert.Equal(t, tt.max, s.Max, "max")
			assert.Equal(t, tt.currency, s.Currency)
			assert.Equal(t, tt.period, s.Period)
		})
	}
}

func TestSalary_NotPay_Empty(t *testing.T) {
	tests := []struct{ name, in string }{
		{"experience years", "Опыт работы 3-5 лет"},
		{"team size", "Команда 300 команд по всему миру"},
		{"year range", "Работаем с 2015-2024"},
		{"phone", "+7 777 123 45 67"},
		{"bare number no context", "Мы обрабатываем 5000 запросов в секунду"},
		{"small dollar figure without period", "We raised $10M from investors and give a $10 credit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.False(t, Salary(tt.in).Known(), "got %+v", Salary(tt.in))
		})
	}
}

func TestMonthly_Conversion_ApproximateUSD(t *testing.T) {
	s := Monthly(models.Salary{Min: 120000, Max: 150000, Currency: "USD", Period: models.PeriodYear})
	assert.Equal(t, 10000, s.MonthlyUSDMin)
	assert.Equal(t, 12500, s.MonthlyUSDMax)

	r := Monthly(models.Salary{Min: 328000, Currency: "RUB", Period: models.PeriodMonth})
	assert.Equal(t, 4000, r.MonthlyUSDMin)
}
