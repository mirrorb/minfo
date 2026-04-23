package handlers

import (
	"strconv"
	"strings"

	"minfo/internal/httpapi/transport"
)

// finalizeProgress 会基于当前快照生成任务结束态的进度结果。
func finalizeProgress(base *transport.TaskProgress, stage, detail string, indeterminate bool) *transport.TaskProgress {
	if base == nil {
		return progressSnapshot(100, stage, detail, 0, 0, indeterminate)
	}

	return progressSnapshot(
		maxFloat(base.Percent, 1),
		stage,
		detail,
		base.Current,
		base.Total,
		indeterminate,
	)
}

// progressSnapshot 会构造一个标准化的任务进度对象。
func progressSnapshot(percent float64, stage, detail string, current, total int, indeterminate bool) *transport.TaskProgress {
	progress := &transport.TaskProgress{
		Percent:       clampPercent(percent),
		Stage:         stage,
		Detail:        detail,
		Indeterminate: indeterminate,
	}
	if current > 0 {
		progress.Current = current
	}
	if total > 0 {
		progress.Total = total
	}
	return progress
}

// scaledProgress 会把整数进度映射到指定宽度的百分比区间。
func scaledProgress(current, total, width int) float64 {
	if total <= 0 || width <= 0 {
		return 0
	}
	boundedCurrent := clampInt(current, 0, total)
	return float64(boundedCurrent) / float64(total) * float64(width)
}

// scaledProgressFloat 会把浮点进度映射到指定宽度的百分比区间。
func scaledProgressFloat(current float64, total, width int) float64 {
	if total <= 0 || width <= 0 {
		return 0
	}
	if current < 0 {
		current = 0
	}
	maxCurrent := float64(total)
	if current > maxCurrent {
		current = maxCurrent
	}
	return current / float64(total) * float64(width)
}

// clampInt 会把整数限制在给定闭区间内。
func clampInt(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

// clampPercent 会把百分比限制在 0-100 区间内。
func clampPercent(value float64) float64 {
	switch {
	case value < 0:
		return 0
	case value > 100:
		return 100
	default:
		return value
	}
}

// parseInt 会从正则匹配结果中安全读取指定索引的整数值。
func parseInt(values []string, index int) int {
	if index < 0 || index >= len(values) {
		return 0
	}
	value, err := strconv.Atoi(strings.TrimSpace(values[index]))
	if err != nil {
		return 0
	}
	return value
}

// progressPercent 会安全读取进度对象中的百分比值。
func progressPercent(progress *transport.TaskProgress) float64 {
	if progress == nil {
		return 0
	}
	return progress.Percent
}

// progressCurrent 会安全读取进度对象中的当前计数。
func progressCurrent(progress *transport.TaskProgress) int {
	if progress == nil {
		return 0
	}
	return progress.Current
}

// progressTotal 会安全读取进度对象中的总计数。
func progressTotal(progress *transport.TaskProgress) int {
	if progress == nil {
		return 0
	}
	return progress.Total
}

// maxInt 会返回两个整数中的较大值。
func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

// maxFloat 会返回两个浮点数中的较大值。
func maxFloat(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
}
