package chart

import (
	"fmt"
	"strings"
	"time"

	"github.com/mtlprog/total/internal/model"
)

const (
	DefaultWidth  = 60
	DefaultHeight = 15
)

// RenderPriceChart creates an ASCII chart of price history.
func RenderPriceChart(points []model.PricePoint, width, height int) string {
	if len(points) == 0 {
		return "No price data available"
	}

	if width <= 0 {
		width = DefaultWidth
	}
	if height <= 0 {
		height = DefaultHeight
	}

	// Find min/max prices for scaling
	minPrice := 0.0
	maxPrice := 1.0 // Prices are always 0-1 for probabilities

	// Create canvas
	canvas := make([][]rune, height)
	for i := range canvas {
		canvas[i] = make([]rune, width)
		for j := range canvas[i] {
			canvas[i][j] = ' '
		}
	}

	// Draw Y-axis labels area (6 chars wide)
	labelWidth := 6

	// Sample points to fit width
	dataWidth := width - labelWidth
	sampledPoints := samplePoints(points, dataWidth)

	// Plot points
	for i, point := range sampledPoints {
		// Scale price to canvas height
		normalizedPrice := (point.PriceYes - minPrice) / (maxPrice - minPrice)
		y := height - 1 - int(normalizedPrice*float64(height-1))
		x := labelWidth + i

		if y >= 0 && y < height && x < width {
			canvas[y][x] = '█'

			// Draw vertical line below the point
			for yy := y + 1; yy < height; yy++ {
				if canvas[yy][x] == ' ' {
					canvas[yy][x] = '│'
				}
			}
		}
	}

	// Build output with Y-axis labels
	var sb strings.Builder

	sb.WriteString("YES Price (Probability)\n")
	sb.WriteString(strings.Repeat("─", width) + "\n")

	for i := 0; i < height; i++ {
		// Calculate price label for this row
		price := maxPrice - (float64(i) / float64(height-1) * (maxPrice - minPrice))
		label := fmt.Sprintf("%4.0f%%", price*100)

		sb.WriteString(label)
		sb.WriteString(" │")
		sb.WriteString(string(canvas[i][labelWidth:]))
		sb.WriteString("\n")
	}

	// X-axis
	sb.WriteString(strings.Repeat(" ", labelWidth))
	sb.WriteString("└")
	sb.WriteString(strings.Repeat("─", dataWidth))
	sb.WriteString("\n")

	// Time labels
	if len(points) > 0 {
		first := points[0].Timestamp.Format("15:04")
		last := points[len(points)-1].Timestamp.Format("15:04")
		padding := dataWidth - len(first) - len(last)
		if padding < 0 {
			padding = 0
		}
		sb.WriteString(strings.Repeat(" ", labelWidth+1))
		sb.WriteString(first)
		sb.WriteString(strings.Repeat(" ", padding))
		sb.WriteString(last)
		sb.WriteString("\n")
	}

	return sb.String()
}

// RenderSimpleBar creates a simple horizontal bar chart for YES/NO prices.
func RenderSimpleBar(priceYes, priceNo float64, width int) string {
	if width <= 0 {
		width = 50
	}

	yesWidth := int(priceYes * float64(width))
	noWidth := width - yesWidth

	var sb strings.Builder

	// YES bar
	sb.WriteString(fmt.Sprintf("YES %5.1f%% ", priceYes*100))
	sb.WriteString(strings.Repeat("█", yesWidth))
	sb.WriteString(strings.Repeat("░", noWidth))
	sb.WriteString("\n")

	// NO bar
	sb.WriteString(fmt.Sprintf("NO  %5.1f%% ", priceNo*100))
	sb.WriteString(strings.Repeat("░", yesWidth))
	sb.WriteString(strings.Repeat("█", noWidth))
	sb.WriteString("\n")

	return sb.String()
}

// samplePoints samples price points to fit a given width.
func samplePoints(points []model.PricePoint, targetCount int) []model.PricePoint {
	if len(points) <= targetCount {
		return points
	}

	result := make([]model.PricePoint, targetCount)
	step := float64(len(points)) / float64(targetCount)

	for i := 0; i < targetCount; i++ {
		idx := int(float64(i) * step)
		if idx >= len(points) {
			idx = len(points) - 1
		}
		result[i] = points[idx]
	}

	return result
}

// GenerateSampleHistory generates sample price history for testing.
func GenerateSampleHistory(hours int, startPrice float64) []model.PricePoint {
	points := make([]model.PricePoint, hours)
	price := startPrice

	for i := 0; i < hours; i++ {
		// Random walk
		change := (float64(i%7) - 3) / 100.0
		price += change
		if price < 0.05 {
			price = 0.05
		}
		if price > 0.95 {
			price = 0.95
		}

		points[i] = model.PricePoint{
			Timestamp: time.Now().Add(-time.Duration(hours-i) * time.Hour),
			PriceYes:  price,
		}
	}

	return points
}
