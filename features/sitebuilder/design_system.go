package sitebuilder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// DesignSystem is the revisioned, engine-owned theme. Token keys are restricted
// to the allow-list below; callers cannot inject selectors or arbitrary CSS.
type DesignSystem struct {
	FormatVersion int               `json:"formatVersion"`
	Tokens        map[string]string `json:"tokens"`
	MobileTokens  map[string]string `json:"mobileTokens"`
	Fonts         []DesignFont      `json:"fonts"`
	FontRoles     map[string]string `json:"fontRoles"`
	BrandLogoID   string            `json:"brandLogoMediaId,omitempty"`
}

type DesignFont struct {
	Family       string `json:"family"`
	MediaID      string `json:"mediaId"`
	Weight       string `json:"weight"`
	Style        string `json:"style"`
	UnicodeRange string `json:"unicodeRange,omitempty"`
}

type DesignSystemState struct {
	Revision  int64        `json:"revision"`
	Draft     DesignSystem `json:"draft"`
	UpdatedAt time.Time    `json:"updatedAt"`
}

type DesignTokenDefinition struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Group   string `json:"group"`
	Kind    string `json:"kind"`
	Default string `json:"default"`
	Min     int    `json:"min,omitempty"`
	Max     int    `json:"max,omitempty"`
	Unit    string `json:"unit,omitempty"`
	Scope   string `json:"scope,omitempty"`
}

var (
	cssColorPattern = regexp.MustCompile(`^(#[0-9a-fA-F]{3,4}|#[0-9a-fA-F]{6}|#[0-9a-fA-F]{8}|rgba?\([0-9 .,%/]+\))$`)
	cssValuePattern = regexp.MustCompile(`^[A-Za-z0-9 .,%()/_+#-]+$`)
	familyPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _-]{0,79}$`)
	weightPattern   = regexp.MustCompile(`^(100|200|300|400|500|600|700|800|900)( (100|200|300|400|500|600|700|800|900))?$`)
	unicodePattern  = regexp.MustCompile(`(?i)^U\+[0-9A-F?]{1,6}(?:-[0-9A-F?]{1,6})?(?:\s*,\s*U\+[0-9A-F?]{1,6}(?:-[0-9A-F?]{1,6})?)*$`)
)

var themeTokenDefinitions = []DesignTokenDefinition{
	{Key: "--color-ink", Label: "Текст", Group: "Цвета", Kind: "color", Default: "#17333a"},
	{Key: "--color-paper", Label: "Бумага", Group: "Цвета", Kind: "color", Default: "#f7f1e3"},
	{Key: "--color-sky", Label: "Небо", Group: "Цвета", Kind: "color", Default: "#347b9f"},
	{Key: "--color-sky-deep", Label: "Глубокое небо", Group: "Цвета", Kind: "color", Default: "#174d68"},
	{Key: "--color-field", Label: "Поле", Group: "Цвета", Kind: "color", Default: "#69784e"},
	{Key: "--color-field-deep", Label: "Тёмное поле", Group: "Цвета", Kind: "color", Default: "#46583f"},
	{Key: "--color-grain", Label: "Зерно", Group: "Цвета", Kind: "color", Default: "#e0b059"},
	{Key: "--color-focus", Label: "Фокус", Group: "Цвета", Kind: "color", Default: "#f4ca68"},
	{Key: "--color-on-image", Label: "Текст на изображении", Group: "Цвета", Kind: "color", Default: "#fffaf0"},
	{Key: "--color-on-image-muted", Label: "Приглушённый текст на изображении", Group: "Цвета", Kind: "color", Default: "rgb(255 250 240 / 78%)"},
	{Key: "--color-line-on-image", Label: "Линии на изображении", Group: "Цвета", Kind: "color", Default: "rgb(255 250 240 / 42%)"},
	{Key: "--color-ink-muted", Label: "Приглушённый текст", Group: "Цвета", Kind: "color", Default: "rgb(23 51 58 / 72%)"},
	{Key: "--color-line-on-paper", Label: "Линии на светлом фоне", Group: "Цвета", Kind: "color", Default: "rgb(23 51 58 / 24%)"},
	{Key: "--color-main-data-surface", Label: "Поверхность основного блока", Group: "Цвета", Kind: "color", Default: "rgb(255 255 255 / 84%)"},
	{Key: "--color-cutout-wash", Label: "Заливка вырезки", Group: "Цвета", Kind: "color", Default: "rgb(255 250 240 / 4%)"},
	{Key: "--color-cutout-wash-hover", Label: "Заливка вырезки при наведении", Group: "Цвета", Kind: "color", Default: "rgb(255 250 240 / 8%)"},
	{Key: "--color-button-surface", Label: "Фон кнопки", Group: "Цвета", Kind: "color", Default: "rgb(255 255 255 / 80%)"},
	{Key: "--color-button-surface-hover", Label: "Фон кнопки при наведении", Group: "Цвета", Kind: "color", Default: "#ffffff"},
	{Key: "--color-button-label", Label: "Текст кнопки", Group: "Цвета", Kind: "color", Default: "#005594"},
	{Key: "--color-hero-title", Label: "Заголовок героя", Group: "Цвета", Kind: "color", Default: "rgb(255 255 255 / 80%)"},
	{Key: "--color-hero-subtitle", Label: "Подзаголовок героя", Group: "Цвета", Kind: "color", Default: "#ffffff"},
	{Key: "--color-page-wash", Label: "Затемнение фона", Group: "Цвета", Kind: "color", Default: "rgb(20 48 58 / 16%)"},
	{Key: "--color-destructive", Label: "Ошибка", Group: "Цвета", Kind: "color", Default: "#a8332e"},
	{Key: "--text-shadow-on-image", Label: "Тень текста на изображении", Group: "Эффекты", Kind: "value", Default: "0 1px 0 var(--color-ink-muted)"},
	{Key: "--page-image-overlay", Label: "Слой поверх фона", Group: "Эффекты", Kind: "value", Default: "linear-gradient(var(--color-page-wash), var(--color-page-wash))"},
	{Key: "--page-fallback", Label: "Градиент-заполнитель фона", Group: "Эффекты", Kind: "value", Default: "linear-gradient(165deg, var(--color-sky-deep) 0%, var(--color-sky) 36%, var(--color-field) 68%, var(--color-field-deep) 100%)"},
	{Key: "--page-background-plain", Label: "Однотонный фон страницы", Group: "Эффекты", Kind: "value", Default: "linear-gradient(var(--color-field-deep), var(--color-field-deep))"},
	{Key: "--page-background-image", Label: "Слой изображения страницы", Group: "Эффекты", Kind: "value", Default: "var(--page-background-plain)"},
	{Key: "--text-caption", Label: "Подпись", Group: "Типографика", Kind: "value", Default: "0.8125rem"},
	{Key: "--text-small", Label: "Мелкий текст", Group: "Типографика", Kind: "value", Default: "0.9375rem"},
	{Key: "--text-body", Label: "Основной текст", Group: "Типографика", Kind: "value", Default: "1rem"},
	{Key: "--text-card-title", Label: "Заголовок карточки", Group: "Типографика", Kind: "value", Default: "1.25rem"},
	{Key: "--text-section-title", Label: "Заголовок секции", Group: "Типографика", Kind: "value", Default: "clamp(2rem, 4.8vw, 4rem)"},
	{Key: "--text-hero-title", Label: "Заголовок героя", Group: "Типографика", Kind: "value", Default: "clamp(4.5rem, 15vw, 10.5rem)"},
	{Key: "--text-hero-subtitle", Label: "Подзаголовок героя", Group: "Типографика", Kind: "value", Default: "clamp(1.25rem, 2vw, 2.5rem)"},
	{Key: "--text-hero-action", Label: "Кнопка героя", Group: "Типографика", Kind: "value", Default: "clamp(1.25rem, 2vw, 2.5rem)"},
	{Key: "--hero-display-size", Label: "Размер логотипа героя", Group: "Типографика", Kind: "value", Default: "clamp(8rem, 14vw, 17.5rem)"},
	{Key: "--hero-display-size-mobile", Label: "Размер логотипа героя на мобильном", Group: "Типографика", Kind: "value", Default: "clamp(7rem, 35vw, 13rem)"},
	{Key: "--line-body", Label: "Межстрочный интервал текста", Group: "Типографика", Kind: "value", Default: "1.55"},
	{Key: "--line-heading", Label: "Межстрочный интервал заголовка", Group: "Типографика", Kind: "value", Default: "0.94"},
	{Key: "--line-hero-display", Label: "Интервал логотипа героя", Group: "Типографика", Kind: "value", Default: "0.78"},
	{Key: "--line-hero-subtitle", Label: "Интервал подзаголовка", Group: "Типографика", Kind: "value", Default: "1"},
	{Key: "--line-button", Label: "Интервал кнопки", Group: "Типографика", Kind: "value", Default: "0.75"},
	{Key: "--tracking-display", Label: "Межбуквенный интервал заголовка", Group: "Типографика", Kind: "value", Default: "-0.065em"},
	{Key: "--tracking-hero-display", Label: "Межбуквенный интервал героя", Group: "Типографика", Kind: "value", Default: "0"},
	{Key: "--tracking-hero-subtitle", Label: "Межбуквенный интервал подзаголовка", Group: "Типографика", Kind: "value", Default: "0"},
	{Key: "--tracking-brand", Label: "Межбуквенный интервал бренда", Group: "Типографика", Kind: "value", Default: "-0.035em"},
	{Key: "--tracking-label", Label: "Межбуквенный интервал меток", Group: "Типографика", Kind: "value", Default: "0.04em"},
	{Key: "--weight-regular", Label: "Обычное начертание", Group: "Типографика", Kind: "value", Default: "400"},
	{Key: "--weight-light", Label: "Лёгкое начертание", Group: "Типографика", Kind: "value", Default: "300"},
	{Key: "--weight-medium", Label: "Среднее начертание", Group: "Типографика", Kind: "value", Default: "500"},
	{Key: "--weight-semibold", Label: "Полужирное начертание", Group: "Типографика", Kind: "value", Default: "600"},
	{Key: "--weight-bold", Label: "Жирное начертание", Group: "Типографика", Kind: "value", Default: "700"},
	{Key: "--space-1", Label: "Шаг 1", Group: "Размеры и сетка", Kind: "value", Default: "0.5rem"},
	{Key: "--space-2", Label: "Шаг 2", Group: "Размеры и сетка", Kind: "value", Default: "0.75rem"},
	{Key: "--space-3", Label: "Шаг 3", Group: "Размеры и сетка", Kind: "value", Default: "1rem"},
	{Key: "--space-4", Label: "Шаг 4", Group: "Размеры и сетка", Kind: "value", Default: "1.5rem"},
	{Key: "--space-5", Label: "Шаг 5", Group: "Размеры и сетка", Kind: "value", Default: "2rem"},
	{Key: "--space-6", Label: "Шаг 6", Group: "Размеры и сетка", Kind: "value", Default: "3rem"},
	{Key: "--space-7", Label: "Шаг 7", Group: "Размеры и сетка", Kind: "value", Default: "4rem"},
	{Key: "--page-gutter", Label: "Отступ страницы", Group: "Размеры и сетка", Kind: "value", Default: "clamp(1.25rem, 5vw, 5rem)"},
	{Key: "--content-width", Label: "Ширина контента", Group: "Размеры и сетка", Kind: "value", Default: "76rem"},
	{Key: "--page-min-height", Label: "Минимальная высота страницы", Group: "Размеры и сетка", Kind: "value", Default: "100svh"},
	{Key: "--minimum-page-width", Label: "Минимальная ширина страницы", Group: "Размеры и сетка", Kind: "value", Default: "20rem"},
	{Key: "--hero-height", Label: "Высота героя", Group: "Размеры и сетка", Kind: "value", Default: "clamp(34rem, 78svh, 58rem)"},
	{Key: "--hero-cow-width", Label: "Ширина коровы", Group: "Размеры и сетка", Kind: "value", Default: "min(90vw, 112rem)"},
	{Key: "--hero-cow-width-mobile", Label: "Ширина коровы на мобильном", Group: "Размеры и сетка", Kind: "value", Default: "min(175vw, 72rem)"},
	{Key: "--hero-cow-width-tablet", Label: "Ширина коровы на планшете", Group: "Размеры и сетка", Kind: "value", Default: "min(150vw, 72rem)"},
	{Key: "--hero-cow-top-offset", Label: "Положение коровы сверху", Group: "Размеры и сетка", Kind: "value", Default: "min(18.58vw, 23.25rem)"},
	{Key: "--hero-wheat-bundle-width", Label: "Ширина колосьев", Group: "Размеры и сетка", Kind: "value", Default: "min(28vw, 34rem)"},
	{Key: "--hero-wheat-bundle-width-mobile", Label: "Ширина колосьев на мобильном", Group: "Размеры и сетка", Kind: "value", Default: "min(58vw, 22rem)"},
	{Key: "--hero-wheat-stalk-width", Label: "Ширина стебля", Group: "Размеры и сетка", Kind: "value", Default: "min(18vw, 24rem)"},
	{Key: "--hero-wheat-stalk-width-mobile", Label: "Ширина стебля на мобильном", Group: "Размеры и сетка", Kind: "value", Default: "min(36vw, 16rem)"},
	{Key: "--hero-wheat-sprig-width", Label: "Ширина колоска", Group: "Размеры и сетка", Kind: "value", Default: "min(14vw, 18rem)"},
	{Key: "--hero-wheat-sprig-width-mobile", Label: "Ширина колоска на мобильном", Group: "Размеры и сетка", Kind: "value", Default: "min(20vw, 6rem)"},
	{Key: "--hero-wheat-sprig-top-mobile", Label: "Положение колоска на мобильном", Group: "Размеры и сетка", Kind: "value", Default: "clamp(3.5rem, 10vw, 4rem)"},
	{Key: "--hero-wheat-stalk-top-mobile", Label: "Положение стебля на мобильном", Group: "Размеры и сетка", Kind: "value", Default: "80vw"},
	{Key: "--hero-action-width", Label: "Ширина кнопки героя", Group: "Размеры и сетка", Kind: "value", Default: "clamp(14rem, 17vw, 22rem)"},
	{Key: "--hero-action-width-mobile", Label: "Ширина кнопки героя на мобильном", Group: "Размеры и сетка", Kind: "value", Default: "clamp(9.5rem, 35vw, 11.5rem)"},
	{Key: "--hero-action-height", Label: "Высота кнопки героя", Group: "Размеры и сетка", Kind: "value", Default: "clamp(3rem, 5.4vw, 6.5rem)"},
	{Key: "--hero-action-height-mobile", Label: "Высота кнопки на мобильном", Group: "Размеры и сетка", Kind: "value", Default: "3rem"},
	{Key: "--button-height", Label: "Высота кнопки", Group: "Размеры и сетка", Kind: "value", Default: "4.875rem"},
	{Key: "--button-gap", Label: "Промежуток кнопки", Group: "Размеры и сетка", Kind: "value", Default: "1rem"},
	{Key: "--button-padding-inline", Label: "Внутренний отступ кнопки", Group: "Размеры и сетка", Kind: "value", Default: "2.5rem"},
	{Key: "--button-text-size", Label: "Размер текста кнопки", Group: "Размеры и сетка", Kind: "value", Default: "2.5rem"},
	{Key: "--form-input-height", Label: "Высота поля формы", Group: "Размеры и сетка", Kind: "value", Default: "2.75rem"},
	{Key: "--button-icon-width", Label: "Ширина значка кнопки", Group: "Размеры и сетка", Kind: "value", Default: "0.875rem"},
	{Key: "--button-icon-height", Label: "Высота значка кнопки", Group: "Размеры и сетка", Kind: "value", Default: "1.5rem"},
	{Key: "--header-height", Label: "Высота шапки", Group: "Размеры и сетка", Kind: "value", Default: "6.25rem"},
	{Key: "--brand-mark-size", Label: "Размер знака бренда", Group: "Размеры и сетка", Kind: "value", Default: "2.75rem"},
	{Key: "--header-logo-size", Label: "Размер логотипа в шапке", Group: "Размеры и сетка", Kind: "value", Default: "4rem"},
	{Key: "--header-content-width", Label: "Ширина содержимого шапки", Group: "Размеры и сетка", Kind: "value", Default: "90rem"},
	{Key: "--header-gutter", Label: "Отступ шапки от края", Group: "Размеры и сетка", Kind: "value", Default: "clamp(1.25rem, 3.5vw, 4rem)"},
	{Key: "--header-grid-left", Label: "Левый столбец шапки", Group: "Размеры и сетка", Kind: "value", Default: "var(--header-gutter)"},
	{Key: "--header-capsule-width", Label: "Ширина капсулы шапки", Group: "Размеры и сетка", Kind: "value", Default: "19rem"},
	{Key: "--header-capsule-height", Label: "Высота капсулы шапки", Group: "Размеры и сетка", Kind: "value", Default: "5rem"},
	{Key: "--header-capsule-top", Label: "Положение капсулы шапки", Group: "Размеры и сетка", Kind: "value", Default: "calc((var(--header-height) - var(--header-capsule-height)) / 2)"},
	{Key: "--header-capsule-content-padding", Label: "Отступ содержимого шапки", Group: "Размеры и сетка", Kind: "value", Default: "1rem"},
	{Key: "--header-navigation-gap", Label: "Интервал навигации", Group: "Размеры и сетка", Kind: "value", Default: "1.5rem"},
	{Key: "--header-navigation-dot-size", Label: "Размер разделителя навигации", Group: "Размеры и сетка", Kind: "value", Default: "0.3125rem"},
	{Key: "--header-navigation-font-size", Label: "Размер текста навигации", Group: "Типографика", Kind: "value", Default: "1.5rem"},
	{Key: "--header-navigation-underline-offset", Label: "Отступ подчёркивания навигации", Group: "Размеры и сетка", Kind: "value", Default: "0.25rem"},
	{Key: "--layer-background", Label: "Слой фона", Group: "Размеры и сетка", Kind: "value", Default: "-1"},
	{Key: "--layer-content", Label: "Слой содержимого", Group: "Размеры и сетка", Kind: "value", Default: "1"},
	{Key: "--layer-header", Label: "Слой шапки", Group: "Размеры и сетка", Kind: "value", Default: "2"},
	{Key: "--section-gap", Label: "Расстояние между секциями", Group: "Размеры и сетка", Kind: "value", Default: "clamp(5rem, 11vw, 10rem)"},
	{Key: "--main-data-section-gap", Label: "Отступ внутри основного блока", Group: "Размеры и сетка", Kind: "value", Default: "clamp(3rem, 6vw, 5rem)"},
	{Key: "--main-data-padding-block", Label: "Внутренний отступ основного блока", Group: "Размеры и сетка", Kind: "value", Default: "clamp(2rem, 5vw, 4.5rem)"},
	{Key: "--main-data-radius", Label: "Скругление основного блока", Group: "Радиусы и границы", Kind: "value", Default: "clamp(1.5rem, 3vw, 3rem)"},
	{Key: "--info-card-width", Label: "Ширина карточки преимуществ", Group: "Размеры и сетка", Kind: "value", Default: "16rem"},
	{Key: "--catalog-card-min-width", Label: "Минимальная ширина товара", Group: "Размеры и сетка", Kind: "value", Default: "10.5rem"},
	{Key: "--hero-title-width", Label: "Ширина заголовка героя", Group: "Размеры и сетка", Kind: "value", Default: "12ch"},
	{Key: "--hero-copy-width", Label: "Ширина текста героя", Group: "Размеры и сетка", Kind: "value", Default: "34rem"},
	{Key: "--info-copy-width", Label: "Ширина текста преимуществ", Group: "Размеры и сетка", Kind: "value", Default: "32rem"},
	{Key: "--product-copy-width", Label: "Ширина описания товара", Group: "Размеры и сетка", Kind: "value", Default: "30rem"},
	{Key: "--product-visual-height", Label: "Высота изображения товара", Group: "Размеры и сетка", Kind: "value", Default: "13rem"},
	{Key: "--product-visual-ratio", Label: "Пропорции изображения товара", Group: "Размеры и сетка", Kind: "value", Default: "4 / 3"},
	{Key: "--footer-padding", Label: "Отступы подвала", Group: "Размеры и сетка", Kind: "value", Default: "clamp(2rem, 5vw, 4rem)"},
	{Key: "--footer-copy-width", Label: "Ширина текста подвала", Group: "Размеры и сетка", Kind: "value", Default: "28rem"},
	{Key: "--form-width", Label: "Ширина формы", Group: "Размеры и сетка", Kind: "value", Default: "28rem"},
	{Key: "--header-backdrop-blur", Label: "Размытие под шапкой", Group: "Эффекты", Kind: "value", Default: "0.25rem"},
	{Key: "--radius-card", Label: "Карточка", Group: "Радиусы и границы", Kind: "value", Default: "1rem"},
	{Key: "--radius-button", Label: "Кнопка", Group: "Радиусы и границы", Kind: "value", Default: "1.5rem"},
	{Key: "--radius-pill", Label: "Капсула", Group: "Радиусы и границы", Kind: "value", Default: "999px"},
	{Key: "--border-thin", Label: "Тонкая граница", Group: "Радиусы и границы", Kind: "value", Default: "1px"},
	{Key: "--button-shadow", Label: "Тень кнопки", Group: "Эффекты", Kind: "value", Default: "0 0.5rem 1.75rem rgb(15 40 49 / 16%)"},
	{Key: "--motion-standard", Label: "Стандартная анимация", Group: "Движение", Kind: "value", Default: "180ms ease"},
	{Key: "--hero-cow-top-offset", Label: "Положение героя на мобильном", Group: "Мобильные размеры", Kind: "value", Default: "var(--space-4)", Scope: "mobile"},
	{Key: "--header-height", Label: "Высота шапки на мобильном", Group: "Мобильные размеры", Kind: "value", Default: "5rem", Scope: "mobile"},
	{Key: "--header-logo-size", Label: "Размер логотипа на мобильном", Group: "Мобильные размеры", Kind: "value", Default: "2.75rem", Scope: "mobile"},
	{Key: "--header-gutter", Label: "Отступ шапки на мобильном", Group: "Мобильные размеры", Kind: "value", Default: "clamp(0.75rem, 3vw, 1.25rem)", Scope: "mobile"},
	{Key: "--header-capsule-width", Label: "Ширина капсулы на мобильном", Group: "Мобильные размеры", Kind: "value", Default: "16rem", Scope: "mobile"},
	{Key: "--header-capsule-height", Label: "Высота капсулы на мобильном", Group: "Мобильные размеры", Kind: "value", Default: "3.5rem", Scope: "mobile"},
	{Key: "--header-capsule-content-padding", Label: "Отступ содержимого шапки на мобильном", Group: "Мобильные размеры", Kind: "value", Default: "0.625rem", Scope: "mobile"},
	{Key: "--header-navigation-gap", Label: "Интервал навигации на мобильном", Group: "Мобильные размеры", Kind: "value", Default: "0.875rem", Scope: "mobile"},
	{Key: "--header-navigation-font-size", Label: "Размер навигации на мобильном", Group: "Мобильные размеры", Kind: "value", Default: "1rem", Scope: "mobile"},
	{Key: "--header-navigation-dot-size", Label: "Размер разделителя на мобильном", Group: "Мобильные размеры", Kind: "value", Default: "0.25rem", Scope: "mobile"},
	{Key: "--button-height", Label: "Высота кнопки на мобильном", Group: "Мобильные размеры", Kind: "value", Default: "3.25rem", Scope: "mobile"},
	{Key: "--button-gap", Label: "Интервал кнопки на мобильном", Group: "Мобильные размеры", Kind: "value", Default: "0.625rem", Scope: "mobile"},
	{Key: "--button-padding-inline", Label: "Отступ кнопки на мобильном", Group: "Мобильные размеры", Kind: "value", Default: "1.625rem", Scope: "mobile"},
	{Key: "--button-text-size", Label: "Размер текста кнопки на мобильном", Group: "Мобильные размеры", Kind: "value", Default: "1.5rem", Scope: "mobile"},
	{Key: "--button-icon-width", Label: "Ширина значка кнопки на мобильном", Group: "Мобильные размеры", Kind: "value", Default: "0.75rem", Scope: "mobile"},
	{Key: "--button-icon-height", Label: "Высота значка кнопки на мобильном", Group: "Мобильные размеры", Kind: "value", Default: "1.25rem", Scope: "mobile"},
	{Key: "--radius-button", Label: "Скругление кнопки на мобильном", Group: "Мобильные размеры", Kind: "value", Default: "1rem", Scope: "mobile"},
	{Key: "--hero-action-height-mobile", Label: "Высота кнопки героя на мобильном", Group: "Мобильные размеры", Kind: "value", Default: "clamp(3.25rem, 10.8vw, 3.5rem)", Scope: "mobile"},
	{Key: "--line-button", Label: "Межстрочный интервал кнопки на мобильном", Group: "Мобильные размеры", Kind: "value", Default: "0.75", Scope: "mobile"},
}

func DesignTokenDefinitions() []DesignTokenDefinition {
	return append([]DesignTokenDefinition(nil), themeTokenDefinitions...)
}

func DefaultDesignSystem() DesignSystem {
	tokens := make(map[string]string, len(themeTokenDefinitions)+4)
	mobileTokens := make(map[string]string)
	for _, token := range themeTokenDefinitions {
		tokens[token.Key] = token.Default
	}
	for _, token := range themeTokenDefinitions {
		if token.Scope == "mobile" {
			delete(tokens, token.Key)
			mobileTokens[token.Key] = token.Default
		}
	}
	// Mobile values override the base token without replacing its desktop value.
	for _, token := range themeTokenDefinitions {
		if token.Scope == "mobile" {
			continue
		}
		if _, exists := tokens[token.Key]; !exists {
			tokens[token.Key] = token.Default
		}
	}
	return DesignSystem{FormatVersion: 1, Tokens: tokens, MobileTokens: mobileTokens, Fonts: []DesignFont{}, FontRoles: map[string]string{"body": "Alumni Sans SC", "display": "Alumni Sans SC", "header": "Alumni Sans SC", "hero": "Alumni Sans SC"}}
}

func NormalizeDesignSystem(value DesignSystem) (DesignSystem, error) {
	if value.FormatVersion != 1 {
		return DesignSystem{}, errors.New("design system formatVersion must be 1")
	}
	defaults := DefaultDesignSystem()
	if value.Tokens == nil {
		value.Tokens = map[string]string{}
	}
	for key := range value.Tokens {
		if _, ok := defaults.Tokens[key]; !ok {
			return DesignSystem{}, fmt.Errorf("unknown design token %q", key)
		}
	}
	for key, fallback := range defaults.Tokens {
		if strings.TrimSpace(value.Tokens[key]) == "" {
			value.Tokens[key] = fallback
		}
	}
	if value.MobileTokens == nil {
		value.MobileTokens = map[string]string{}
	}
	for key := range value.MobileTokens {
		if _, ok := defaults.MobileTokens[key]; !ok {
			return DesignSystem{}, fmt.Errorf("unknown mobile design token %q", key)
		}
	}
	for key, fallback := range defaults.MobileTokens {
		if strings.TrimSpace(value.MobileTokens[key]) == "" {
			value.MobileTokens[key] = fallback
		}
	}
	if value.Fonts == nil {
		value.Fonts = []DesignFont{}
	}
	if value.FontRoles == nil {
		value.FontRoles = defaults.FontRoles
	}
	if value.BrandLogoID != "" {
		if _, err := uuid.Parse(value.BrandLogoID); err != nil {
			return DesignSystem{}, errors.New("brandLogoMediaId must reference a media object")
		}
	}
	fonts := make(map[string]bool, len(value.Fonts))
	fontFaces := make(map[string]bool, len(value.Fonts))
	for i, font := range value.Fonts {
		if !familyPattern.MatchString(font.Family) {
			return DesignSystem{}, fmt.Errorf("font %d has an invalid family name", i)
		}
		id, err := uuid.Parse(font.MediaID)
		if err != nil {
			return DesignSystem{}, fmt.Errorf("font %q must reference a media object", font.Family)
		}
		_ = id
		if !weightPattern.MatchString(font.Weight) {
			return DesignSystem{}, fmt.Errorf("font %q has an invalid weight range", font.Family)
		}
		if font.Style != "normal" && font.Style != "italic" {
			return DesignSystem{}, fmt.Errorf("font %q style must be normal or italic", font.Family)
		}
		if font.UnicodeRange != "" && !unicodePattern.MatchString(font.UnicodeRange) {
			return DesignSystem{}, fmt.Errorf("font %q has an invalid unicode range", font.Family)
		}
		faceKey := font.Family + "\x00" + font.Weight + "\x00" + font.Style + "\x00" + font.UnicodeRange
		if fontFaces[faceKey] {
			return DesignSystem{}, fmt.Errorf("font face %q at weight %s (%s) is duplicated", font.Family, font.Weight, font.Style)
		}
		fonts[font.Family] = true
		fontFaces[faceKey] = true
	}
	for role, family := range value.FontRoles {
		if role != "body" && role != "display" && role != "header" && role != "hero" {
			return DesignSystem{}, fmt.Errorf("unknown font role %q", role)
		}
		if !familyPattern.MatchString(family) {
			return DesignSystem{}, fmt.Errorf("font role %q has an invalid family", role)
		}
		if family != "sans-serif" && family != "serif" && family != "system-ui" && family != "Alumni Sans SC" && !fonts[family] {
			return DesignSystem{}, fmt.Errorf("font role %q references an unavailable font family", role)
		}
	}
	for _, token := range themeTokenDefinitions {
		v := value.Tokens[token.Key]
		if token.Scope == "mobile" {
			v = value.MobileTokens[token.Key]
		}
		if token.Kind == "color" {
			if !cssColorPattern.MatchString(v) {
				return DesignSystem{}, fmt.Errorf("%s must be a hex or rgb color", token.Key)
			}
		} else if !cssValuePattern.MatchString(v) || strings.Contains(strings.ToLower(v), "url") || strings.Contains(strings.ToLower(v), "expression") || strings.Contains(v, "\\") {
			return DesignSystem{}, fmt.Errorf("%s contains an unsupported CSS value", token.Key)
		}
	}
	return value, nil
}

func (s *SnapshotService) GetDesignSystem(ctx context.Context) (DesignSystemState, error) {
	defaults, _ := json.Marshal(DefaultDesignSystem())
	if _, err := s.pool.Exec(ctx, `INSERT INTO site_design_system_state (id,draft,revision) VALUES (TRUE,$1::jsonb,1) ON CONFLICT (id) DO NOTHING`, string(defaults)); err != nil {
		return DesignSystemState{}, err
	}
	return readDesignSystem(ctx, s.pool)
}

func readDesignSystem(ctx context.Context, queryer metadataQueryer) (DesignSystemState, error) {
	var state DesignSystemState
	var raw []byte
	err := queryer.QueryRow(ctx, `SELECT revision,draft,updated_at FROM site_design_system_state WHERE id=TRUE`).Scan(&state.Revision, &raw, &state.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return DesignSystemState{}, ErrNotFound
	}
	if err != nil {
		return DesignSystemState{}, err
	}
	if err := json.Unmarshal(raw, &state.Draft); err != nil {
		return DesignSystemState{}, err
	}
	state.Draft, err = NormalizeDesignSystem(state.Draft)
	return state, err
}

func (s *SnapshotService) SaveDesignSystem(ctx context.Context, actor uuid.UUID, expected int64, raw json.RawMessage) (DesignSystemState, error) {
	value, err := decodeDesignSystem(raw)
	if err != nil {
		return DesignSystemState{}, fmt.Errorf("%w: decode design system: %v", ErrInvalid, err)
	}
	value, err = NormalizeDesignSystem(value)
	if err != nil {
		return DesignSystemState{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return DesignSystemState{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	defaults, _ := json.Marshal(DefaultDesignSystem())
	if _, err := tx.Exec(ctx, `INSERT INTO site_design_system_state (id,draft,revision) VALUES (TRUE,$1::jsonb,1) ON CONFLICT (id) DO NOTHING`, string(defaults)); err != nil {
		return DesignSystemState{}, err
	}
	var current int64
	if err := tx.QueryRow(ctx, `SELECT revision FROM site_design_system_state WHERE id=TRUE FOR UPDATE`).Scan(&current); err != nil {
		return DesignSystemState{}, err
	}
	if current != expected {
		return DesignSystemState{}, fmt.Errorf("%w: expected design-system revision %d, current %d", ErrConflict, expected, current)
	}
	if err := validateDesignSystemMedia(ctx, tx, value); err != nil {
		return DesignSystemState{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return DesignSystemState{}, err
	}
	var state DesignSystemState
	var result []byte
	err = tx.QueryRow(ctx, `UPDATE site_design_system_state SET draft=$1::jsonb,revision=revision+1,updated_by=$2,updated_at=now() WHERE id=TRUE RETURNING revision,draft,updated_at`, string(encoded), actor).Scan(&state.Revision, &result, &state.UpdatedAt)
	if err != nil {
		return DesignSystemState{}, err
	}
	if err := json.Unmarshal(result, &state.Draft); err != nil {
		return DesignSystemState{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return DesignSystemState{}, err
	}
	return state, nil
}

func decodeDesignSystem(raw json.RawMessage) (DesignSystem, error) {
	var value DesignSystem
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return DesignSystem{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return DesignSystem{}, errors.New("design system contains trailing JSON")
		}
		return DesignSystem{}, err
	}
	return value, nil
}

func validateDesignSystemMedia(ctx context.Context, queryer metadataQueryer, value DesignSystem) error {
	ids := []struct{ id, mime string }{}
	if value.BrandLogoID != "" {
		ids = append(ids, struct{ id, mime string }{value.BrandLogoID, "image/"})
	}
	for _, font := range value.Fonts {
		ids = append(ids, struct{ id, mime string }{font.MediaID, "font/woff2"})
	}
	seen := map[string]bool{}
	for _, item := range ids {
		if seen[item.id] {
			continue
		}
		seen[item.id] = true
		id, err := uuid.Parse(item.id)
		if err != nil {
			return err
		}
		var mime string
		if err := queryer.QueryRow(ctx, `SELECT content_type FROM media_objects WHERE id=$1 AND status='ready'`, id).Scan(&mime); err != nil {
			return fmt.Errorf("system asset %s is unavailable", item.id)
		}
		if item.mime == "image/" {
			if !strings.HasPrefix(mime, "image/") {
				return fmt.Errorf("brand logo %s must be an image", item.id)
			}
		} else if mime != item.mime {
			return fmt.Errorf("font %s must use %s", item.id, item.mime)
		}
	}
	return nil
}

func (value DesignSystem) CSS(themeBaseURL string) (string, error) {
	normalized, err := NormalizeDesignSystem(value)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	keys := make([]string, 0, len(normalized.Tokens))
	for key := range normalized.Tokens {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	b.WriteString(":root{")
	for _, key := range keys {
		b.WriteString(key)
		b.WriteByte(':')
		b.WriteString(normalized.Tokens[key])
		b.WriteByte(';')
	}
	for _, role := range []string{"body", "display", "header", "hero"} {
		family := normalized.FontRoles[role]
		b.WriteString("--font-")
		b.WriteString(role)
		b.WriteString(":\"")
		b.WriteString(family)
		b.WriteString("\",sans-serif;")
	}
	b.WriteString("--font-alumni:\"Alumni Sans SC\",sans-serif;color-scheme:light;")
	b.WriteString("--background:var(--color-paper);--foreground:var(--color-ink);--card:var(--color-paper);--card-foreground:var(--color-ink);--popover:var(--color-paper);--popover-foreground:var(--color-ink);--primary:var(--color-button-surface);--primary-foreground:var(--color-button-label);--secondary:var(--color-sky-deep);--secondary-foreground:var(--color-on-image);--muted:var(--color-field-deep);--muted-foreground:var(--color-on-image-muted);--accent:var(--color-grain);--accent-foreground:var(--color-ink);--destructive:var(--color-destructive);--border:var(--color-line-on-image);--input:var(--color-line-on-image);--ring:var(--color-focus);--radius:var(--radius-card);}")
	fonts := append([]DesignFont(nil), normalized.Fonts...)
	sort.Slice(fonts, func(i, j int) bool {
		if fonts[i].Family != fonts[j].Family {
			return fonts[i].Family < fonts[j].Family
		}
		if fonts[i].Weight != fonts[j].Weight {
			return fonts[i].Weight < fonts[j].Weight
		}
		if fonts[i].Style != fonts[j].Style {
			return fonts[i].Style < fonts[j].Style
		}
		return fonts[i].UnicodeRange < fonts[j].UnicodeRange
	})
	for _, font := range fonts {
		assetRoot := strings.TrimRight(themeBaseURL, "/")
		if assetRoot == "" {
			assetRoot = "/api/v2/system-assets"
		}
		fmt.Fprintf(&b, `@font-face{font-family:"%s";font-style:%s;font-weight:%s;font-display:swap;src:url("%s/%s") format("woff2");`, font.Family, font.Style, font.Weight, assetRoot, font.MediaID)
		if font.UnicodeRange != "" {
			fmt.Fprintf(&b, `unicode-range:%s;`, font.UnicodeRange)
		}
		b.WriteByte('}')
	}
	if len(normalized.MobileTokens) > 0 {
		mobileKeys := make([]string, 0, len(normalized.MobileTokens))
		for key := range normalized.MobileTokens {
			mobileKeys = append(mobileKeys, key)
		}
		sort.Strings(mobileKeys)
		b.WriteString("@media(max-width:48rem){:root{")
		for _, key := range mobileKeys {
			b.WriteString(key)
			b.WriteByte(':')
			b.WriteString(normalized.MobileTokens[key])
			b.WriteByte(';')
		}
		b.WriteString("}}")
	}
	return b.String(), nil
}

type designPreview struct {
	value   DesignSystem
	expires time.Time
}

func (s *SnapshotService) CreateDesignPreview(ctx context.Context, raw json.RawMessage) (string, error) {
	value, err := decodeDesignSystem(raw)
	if err != nil {
		return "", fmt.Errorf("%w: decode design system: %v", ErrInvalid, err)
	}
	value, err = NormalizeDesignSystem(value)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if err := validateDesignSystemMedia(ctx, s.pool, value); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	token := uuid.NewString()
	s.previewMu.Lock()
	if s.previews == nil {
		s.previews = map[string]designPreview{}
	}
	s.previews[token] = designPreview{value: value, expires: time.Now().Add(20 * time.Minute)}
	s.previewMu.Unlock()
	return token, nil
}

func (s *SnapshotService) designPreview(token string) (DesignSystem, bool) {
	s.previewMu.Lock()
	defer s.previewMu.Unlock()
	preview, ok := s.previews[token]
	if !ok {
		return DesignSystem{}, false
	}
	if time.Now().After(preview.expires) {
		delete(s.previews, token)
		return DesignSystem{}, false
	}
	return preview.value, true
}

// DesignPreview is the read-only counterpart used by the public CSS handler.
func (s *SnapshotService) DesignPreview(token string) (DesignSystem, bool) {
	return s.designPreview(token)
}

func (s *SnapshotService) ThemeForSnapshot(ctx context.Context, id uuid.UUID) (DesignSystem, error) {
	var raw []byte
	if err := s.pool.QueryRow(ctx, `SELECT payload FROM site_snapshots WHERE id=$1`, id).Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return DesignSystem{}, ErrNotFound
		}
		return DesignSystem{}, err
	}
	var payload SnapshotPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return DesignSystem{}, err
	}
	if payload.DesignSystem == nil {
		return DesignSystem{}, ErrNotFound
	}
	return NormalizeDesignSystem(*payload.DesignSystem)
}
