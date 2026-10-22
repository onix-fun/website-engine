package settings

// roles mirrors the access rules implemented by the server: RequireStaff and
// RequireAdmin middleware plus the per-resource checks in the domain
// services. It is a description of the current behaviour, not an aspirational
// model.
func roles() []Role {
	return []Role{
		{
			ID:      "admin",
			Label:   "Администратор",
			Summary: "Полный доступ, включая prod-релизы и управление доступами.",
			Permissions: []RolePermission{
				{Resource: "Features", Actions: []string{"просмотр", "создание", "переименование", "архивирование"}},
				{Resource: "Схемы", Actions: []string{"просмотр", "редактирование", "публикация в dev", "promote в prod", "откат dev/prod"}},
				{Resource: "Контент", Actions: []string{"просмотр", "редактирование", "публикация в dev", "promote в prod", "откат dev/prod", "снятие с публикации", "архивирование"}},
				{Resource: "Заявки", Actions: []string{"просмотр", "смена статуса", "архивирование", "удаление"}},
				{Resource: "Медиа", Actions: []string{"просмотр", "загрузка", "переименование", "удаление"}},
				{Resource: "Релизы", Actions: []string{"просмотр", "импорт", "активация dev/prod", "откат dev/prod", "источники"}},
				{Resource: "Токены", Actions: []string{"просмотр всех", "создание любой роли", "отзыв"}},
				{Resource: "Настройки", Actions: []string{"просмотр"}},
			},
			Notes: []string{
				"Единственная роль, которой доступно удаление заявок и активация/откат релизов в prod.",
				"Видит все токены, независимо от того, кто их создал.",
			},
		},
		{
			ID:      "moderator",
			Label:   "Модератор",
			Summary: "Схемы, контент dev/prod, импорт и активация релизов в dev.",
			Permissions: []RolePermission{
				{Resource: "Features", Actions: []string{"просмотр", "создание", "переименование", "архивирование"}},
				{Resource: "Схемы", Actions: []string{"просмотр", "редактирование", "публикация в dev", "promote в prod", "откат dev"}},
				{Resource: "Контент", Actions: []string{"просмотр", "редактирование", "публикация в dev", "promote в prod", "откат dev", "снятие с публикации dev", "архивирование"}},
				{Resource: "Заявки", Actions: []string{"просмотр", "смена статуса", "архивирование"}},
				{Resource: "Медиа", Actions: []string{"просмотр", "загрузка", "переименование", "удаление"}},
				{Resource: "Релизы", Actions: []string{"просмотр", "импорт", "активация dev", "источники"}},
				{Resource: "Токены", Actions: []string{"просмотр созданных", "создание роли user", "отзыв созданных"}},
				{Resource: "Настройки", Actions: []string{"просмотр"}},
			},
			Notes: []string{
				"Видит только те токены, которые создал сам.",
				"Не может создавать токены с ролями admin и moderator.",
				"Не может активировать или откатывать релизы в prod.",
				"Удаление заявок недоступно.",
			},
		},
		{
			ID:      "user",
			Label:   "Пользователь",
			Summary: "Контент существующих фич и dev-публикации, медиа, просмотр заявок.",
			Permissions: []RolePermission{
				{Resource: "Features", Actions: []string{"просмотр"}},
				{Resource: "Схемы", Actions: []string{"просмотр"}},
				{Resource: "Контент", Actions: []string{"просмотр", "создание", "редактирование", "публикация в dev", "снятие с публикации dev"}},
				{Resource: "Заявки", Actions: []string{"просмотр", "экспорт"}},
				{Resource: "Медиа", Actions: []string{"просмотр", "загрузка", "переименование"}},
				{Resource: "Релизы", Actions: []string{"просмотр"}},
				{Resource: "Токены", Actions: []string{}},
				{Resource: "Настройки", Actions: []string{}},
			},
			Notes: []string{
				"Не может создавать фичи, менять схемы и настройки Feature.",
				"Не может продвигать контент в prod, откатывать версии и архивировать.",
			},
		},
	}
}
