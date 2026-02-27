package main

import (
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// --- ХРАНИЛИЩЕ ДАННЫХ ---
var userStates = make(map[int64]string)
var tempTemplateData = make(map[int64]string)
var userSettings = make(map[int64]map[string]string)

func main() {
	// Твой токен
	bot, err := tgbotapi.NewBotAPI("TOKKEN")
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = true
	log.Printf("Авторизован на аккаунте %s", bot.Self.UserName)

	// Настройка команд меню
	commands := []tgbotapi.BotCommand{
		{Command: "start", Description: "Начало работы / Start"},
		{Command: "menu", Description: "Главное меню / Main Menu"},
		{Command: "refill", Description: "Баланс / Balance"},
		{Command: "help", Description: "Помощь / Help"},
	}
	bot.Request(tgbotapi.NewSetMyCommands(commands...))

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		// --- 1. КНОПКИ (CALLBACK) ---
		if update.CallbackQuery != nil {
			chatID := update.CallbackQuery.Message.Chat.ID
			messageID := update.CallbackQuery.Message.MessageID
			data := update.CallbackQuery.Data

			// Убираем часики загрузки
			bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))

			// --- ОБРАБОТКА ДИНАМИЧЕСКИХ КНОПОК (Размер чанка) ---
			if strings.HasPrefix(data, "set_chunk_") {
				value := strings.TrimPrefix(data, "set_chunk_")
				saveSetting(chatID, "chunk_size", value)
				sendTemplateSettingsMenu(bot, chatID, messageID, getTemplateName(chatID), "success", "Размер чанка: "+value)
				continue
			}

			switch data {
			// === НАВИГАЦИЯ: ГЛАВНОЕ МЕНЮ ===
			case "btn_main_menu", "btn_topup":
				delete(userStates, chatID)
				delete(tempTemplateData, chatID)
				sendMainMenu(bot, chatID, update.CallbackQuery.From.FirstName)

			// === СМЕНА ЯЗЫКА ===
			case "btn_language":
				sendLanguageSelection(bot, chatID, messageID)
			
			case "set_lang_ru":
				saveSetting(chatID, "lang", "ru")
				// Возвращаем в меню уже на русском
				sendMainMenu(bot, chatID, update.CallbackQuery.From.FirstName)

			case "set_lang_en":
				saveSetting(chatID, "lang", "en")
				// Возвращаем в меню уже на английском
				sendMainMenu(bot, chatID, update.CallbackQuery.From.FirstName)


			// === КНОПКА: ГЕНЕРИМ АУДИО ===
			case "btn_gen_audio":
				userStates[chatID] = "waiting_for_text"
				// Пример простой локализации сообщения
				lang := getSetting(chatID, "lang", "ru")
				var msgText string
				if lang == "en" {
					msgText = "ℹ️ *Instructions:*\n\n✅ *Method 1: File* (.txt, UTF-8)\n✅ *Method 2: Text message*\n\n📌 _Send text to convert..._"
				} else {
					msgText = "ℹ️ *Инструкция по отправке:*\n\n✅ *Способ 1: Файлом* (.txt, UTF-8)\n✅ *Способ 2: Сообщением в чат*\n\n📌 _Пришлите текст для озвучки..._"
				}
				
				msg := tgbotapi.NewMessage(chatID, msgText)
				msg.ParseMode = "Markdown"
				// Кнопка "Главное меню" тоже должна быть переведена, но пока оставим универсальную
				msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
					tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("🏠 Home / Меню", "btn_main_menu")),
				)
				bot.Send(msg)

			// === РАЗДЕЛ КАРТИНОК: ГЛАВНОЕ МЕНЮ ===
			case "btn_gen_image":
				delete(userStates, chatID)
				lang := getSetting(chatID, "lang", "ru")
				
				var text, btnCreate, btnEdit, btnRemix, btnHome string
				if lang == "en" {
					text = "🖼 *Image Generation*\nChoose mode ⤵️"
					btnCreate = "🖼 Create"
					btnEdit = "✏️ Edit"
					btnRemix = "🧩 Remix"
					btnHome = "🏠 Main Menu"
				} else {
					text = "🖼 *Генерация изображений*\nВыберите режим работы ⤵️"
					btnCreate = "🖼 Создать"
					btnEdit = "✏️ Редактировать"
					btnRemix = "🧩 Ремикс"
					btnHome = "🏠 Главное меню"
				}

				msg := tgbotapi.NewMessage(chatID, text)
				msg.ParseMode = "Markdown"
				keyboard := tgbotapi.NewInlineKeyboardMarkup(
					tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(btnCreate, "btn_img_create")),
					tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(btnEdit, "btn_img_edit")),
					tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(btnRemix, "btn_img_remix")),
					tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(btnHome, "btn_main_menu")),
				)
				msg.ReplyMarkup = keyboard
				bot.Send(msg)

			// --- ПОДМЕНЮ КАРТИНОК ---
			case "btn_img_create":
				userStates[chatID] = "waiting_for_img_prompt"
				msg := tgbotapi.NewMessage(chatID, "🎨 *Введите промпт / Enter prompt*")
				msg.ParseMode = "Markdown"
				msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
					tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("❌ Cancel / Отмена", "btn_gen_image")),
				)
				bot.Send(msg)

			case "btn_img_edit":
				userStates[chatID] = "waiting_for_img_edit"
				msg := tgbotapi.NewMessage(chatID, "📤 *Пришлите фото / Send photo*")
				msg.ParseMode = "Markdown"
				msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
					tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("❌ Cancel / Отмена", "btn_gen_image")),
				)
				bot.Send(msg)

			case "btn_img_remix":
				userStates[chatID] = "waiting_for_img_remix"
				msg := tgbotapi.NewMessage(chatID, "🧩 *Пришлите фото (2+) / Send photos*")
				msg.ParseMode = "Markdown"
				msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
					tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("❌ Cancel / Отмена", "btn_gen_image")),
				)
				bot.Send(msg)


			// === КНОПКА: ШАБЛОНЫ (СПИСОК) ===
			case "btn_templates", "btn_back_to_templates":
				delete(userStates, chatID)
				sendTemplatesList(bot, chatID)

			// === СОЗДАНИЕ ШАБЛОНА ===
			case "btn_create_template":
				userStates[chatID] = "waiting_for_template_name"
				sendInputNamePrompt(bot, chatID)

			case "btn_confirm_template_create":
				name := tempTemplateData[chatID]
				setDefaultSettings(chatID, name)
				sendTemplateSettingsMenu(bot, chatID, 0, name, "", "") 
				delete(userStates, chatID)
				delete(tempTemplateData, chatID)

			// === НАСТРОЙКИ ГОЛОСА (ПОДМЕНЮ) ===
			case "tpl_voice_settings":
				sendVoiceSettingsSubmenu(bot, chatID, messageID)

			// === ВВОД ПАРАМЕТРОВ ГОЛОСА ===
			case "set_v_stability":
				userStates[chatID] = "waiting_for_stability"
				current := getSetting(chatID, "stability", "0.5")
				sendInputPrompt(bot, chatID, "⚖️ Устойчивость (Stability)", current, "Отвечает за вариативность. Чем выше, тем ровнее.", "0.0 - 1.0")

			case "set_v_similarity":
				userStates[chatID] = "waiting_for_similarity"
				current := getSetting(chatID, "similarity", "0.75")
				sendInputPrompt(bot, chatID, "🎭 Точность клонирования", current, "Влияет на сходство с оригиналом.", "0.0 - 1.0")

			case "set_v_style":
				userStates[chatID] = "waiting_for_style"
				current := getSetting(chatID, "style", "0.0")
				sendInputPrompt(bot, chatID, "🎨 Экспрессия (Style)", current, "Эмоциональная окраска речи.", "0.0 - 1.0")

			case "set_v_speed":
				userStates[chatID] = "waiting_for_speed"
				current := getSetting(chatID, "speed", "1.0")
				sendInputPrompt(bot, chatID, "⏩ Темп речи (Speed)", current, "Коэффициент скорости.", "0.7 - 1.2")

			case "set_v_boost_toggle":
				current := getSetting(chatID, "boost", "true")
				var newVal, statusMsg string
				if current == "true" {
					newVal = "false"
					statusMsg = "Выключено 🔕"
				} else {
					newVal = "true"
					statusMsg = "Включено 🔊"
				}
				saveSetting(chatID, "boost", newVal)
				sendTemplateSettingsMenu(bot, chatID, messageID, getTemplateName(chatID), "success", "Усиление голоса: "+statusMsg)

			// === ФОРМАТ ОТВЕТА ===
			case "tpl_format":
				sendFormatSelection(bot, chatID, messageID)
			case "set_fmt_single":
				saveSetting(chatID, "format", "single")
				sendTemplateSettingsMenu(bot, chatID, messageID, getTemplateName(chatID), "success", "🎧 Единый файл")
			case "set_fmt_chunk":
				saveSetting(chatID, "format", "chunks")
				sendTemplateSettingsMenu(bot, chatID, messageID, getTemplateName(chatID), "success", "🧩 Нарезка (Chunks)")
			case "set_fmt_para":
				saveSetting(chatID, "format", "paragraphs")
				sendTemplateSettingsMenu(bot, chatID, messageID, getTemplateName(chatID), "success", "¶ По абзацам")

			// === РАЗМЕР ЧАНКА (КНОПКАМИ) ===
			case "tpl_chunk_size":
				sendChunkSizeSelection(bot, chatID, messageID)

			// === ПАУЗЫ ===
			case "tpl_pause_chunk":
				current := getSetting(chatID, "pause_enabled", "false")
				var newVal, statusMsg string
				if current == "true" {
					newVal = "false"
					statusMsg = "Отключено 🔕"
				} else {
					newVal = "true"
					statusMsg = "Включено ⏸"
				}
				saveSetting(chatID, "pause_enabled", newVal)
				sendTemplateSettingsMenu(bot, chatID, messageID, getTemplateName(chatID), "success", "Паузы: "+statusMsg)

			case "tpl_pause_len":
				userStates[chatID] = "waiting_for_pause_len"
				current := getSetting(chatID, "pause_duration", "1")
				sendInputPrompt(bot, chatID, "⏱ Время тишины (сек)", current, "Длительность паузы между файлами.", "1 - 5")

			// === ПОИСК ГОЛОСА ===
			case "tpl_search_voice":
				userStates[chatID] = "waiting_for_voice_search"
				msg := tgbotapi.NewMessage(chatID, "🔎 *Поиск голоса*\n\n"+
					"Пришлите Название, ID голоса или ссылку на голос ElevenLabs.\n"+
					"_Например: Adam или ссылку..._")
				msg.ParseMode = "Markdown"
				msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
					tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", "cancel_input")),
				)
				bot.Send(msg)

			// === УПРАВЛЕНИЕ ШАБЛОНОМ (СВОДКА) ===
			case "tpl_manage":
				sendTemplateManagementMenu(bot, chatID, messageID)

			// --- Действия в управлении ---
			case "btn_edit_name":
				userStates[chatID] = "waiting_for_new_name"
				msg := tgbotapi.NewMessage(chatID, "✏️ Введите *новое название* для шаблона:")
				msg.ParseMode = "Markdown"
				msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
					tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", "tpl_manage")),
				)
				bot.Send(msg)

			case "btn_reset_settings":
				name := getTemplateName(chatID)
				setDefaultSettings(chatID, name)
				sendTemplateManagementMenu(bot, chatID, messageID)

			case "btn_delete_template":
				delete(userSettings, chatID)
				sendTemplatesList(bot, chatID)

			// ОТМЕНА ВВОДА / НАЗАД
			case "cancel_input":
				delete(userStates, chatID)
				sendMainMenu(bot, chatID, update.CallbackQuery.From.FirstName)

			case "back_to_tpl_settings":
				sendTemplateSettingsMenu(bot, chatID, messageID, getTemplateName(chatID), "", "")

			// ВЫБОР МОДЕЛИ
			case "tpl_model":
				sendModelSelection(bot, chatID, messageID)
			case "set_model_eleven_v3":
				saveSetting(chatID, "model", "Eleven v3")
				sendTemplateSettingsMenu(bot, chatID, messageID, getTemplateName(chatID), "success", "Eleven v3")
			case "set_model_multilingual_v2":
				saveSetting(chatID, "model", "Multilingual v2")
				sendTemplateSettingsMenu(bot, chatID, messageID, getTemplateName(chatID), "success", "Multilingual v2")

			default:
				// Заглушка
			}
			continue
		}

		// --- 2. ОБРАБОТКА ТЕКСТА ---
		if update.Message != nil {
			chatID := update.Message.Chat.ID
			text := update.Message.Text

			if update.Message.IsCommand() {
				delete(userStates, chatID)
				switch update.Message.Command() {
				case "start":
					sendMainMenu(bot, chatID, update.Message.From.FirstName)
				case "menu":
					sendMainMenu(bot, chatID, update.Message.From.FirstName)
				case "refill":
					bot.Send(tgbotapi.NewMessage(chatID, "💳 Функция пополнения баланса доступна в главном меню."))
				case "help":
					helpText := "🛠 *Поддержка*\n\n" +
						"Если у вас появились проблемы:\n\n" +
						"Наш канал: [YO-YO Studio](https://t.me/yoyoserv)\n" +
						"Персональная помощь: [Emil](https://t.me/YO_YO_Emil)"
					msg := tgbotapi.NewMessage(chatID, helpText)
					msg.ParseMode = "Markdown"
					msg.DisableWebPagePreview = true
					bot.Send(msg)
				}
				continue
			}

			state := userStates[chatID]

			// Обработка данных для картинок
			if state == "waiting_for_img_prompt" {
				bot.Send(tgbotapi.NewMessage(chatID, "✅ Промпт принят! (Заглушка)"))
				delete(userStates, chatID)
				continue
			}
			if state == "waiting_for_img_edit" || state == "waiting_for_img_remix" {
				if update.Message.Photo != nil {
					bot.Send(tgbotapi.NewMessage(chatID, "✅ Фото принято! (Заглушка)"))
					delete(userStates, chatID)
				} else {
					bot.Send(tgbotapi.NewMessage(chatID, "❌ Пришлите фото!"))
				}
				continue
			}

			// Шаблоны
			if state == "waiting_for_template_name" {
				handleTemplateNameInput(bot, chatID, text)
				continue
			}
			if state == "waiting_for_new_name" {
				saveSetting(chatID, "template_name", text)
				delete(userStates, chatID)
				sendTemplateManagementMenu(bot, chatID, 0)
				continue
			}
			if state == "waiting_for_voice_search" {
				saveSetting(chatID, "voice_id", text)
				delete(userStates, chatID)
				sendTemplateSettingsMenu(bot, chatID, 0, getTemplateName(chatID), "success", "Голос обновлен")
				continue
			}

			// Числа
			handleIntInput := func(settingKey, minStr, maxStr string, minVal, maxVal int, paramName string) {
				cleanText := strings.Replace(text, " ", "", -1)
				val, err := strconv.Atoi(cleanText)
				if err != nil || val < minVal || val > maxVal {
					bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ *Ошибка!*\nВведите целое число от %s до %s", minStr, maxStr)))
					return
				}
				saveSetting(chatID, settingKey, fmt.Sprintf("%d", val))
				delete(userStates, chatID)
				sendTemplateSettingsMenu(bot, chatID, 0, getTemplateName(chatID), "success", fmt.Sprintf("%s: %d", paramName, val))
			}

			handleFloatInput := func(settingKey, minStr, maxStr string, minVal, maxVal float64, paramName string) {
				cleanText := strings.Replace(text, ",", ".", -1)
				val, err := strconv.ParseFloat(cleanText, 64)
				if err != nil || val < minVal || val > maxVal {
					bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ *Ошибка!*\nВведите число от %s до %s", minStr, maxStr)))
					return
				}
				saveSetting(chatID, settingKey, fmt.Sprintf("%.2f", val))
				delete(userStates, chatID)
				sendTemplateSettingsMenu(bot, chatID, 0, getTemplateName(chatID), "success", fmt.Sprintf("%s: %.2f", paramName, val))
			}

			switch state {
			case "waiting_for_stability":
				handleFloatInput("stability", "0", "1", 0.0, 1.0, "⚖️ Устойчивость")
			case "waiting_for_similarity":
				handleFloatInput("similarity", "0", "1", 0.0, 1.0, "🎭 Точность")
			case "waiting_for_style":
				handleFloatInput("style", "0", "1", 0.0, 1.0, "🎨 Экспрессия")
			case "waiting_for_speed":
				handleFloatInput("speed", "0.7", "1.2", 0.7, 1.2, "⏩ Скорость")
			case "waiting_for_pause_len":
				handleIntInput("pause_duration", "1", "5", 1, 5, "⏱ Длина паузы")
			}
		}
	}
}

// --- ФУНКЦИИ ИНТЕРФЕЙСА (ОБНОВЛЕННЫЕ) ---

// 1. ГЛАВНОЕ МЕНЮ С ЛОКАЛИЗАЦИЕЙ И НОВЫМ ПОРЯДКОМ
func sendMainMenu(bot *tgbotapi.BotAPI, chatID int64, userName string) {
	lang := getSetting(chatID, "lang", "ru")

	var text, btnAudio, btnTemplates, btnImg, btnTopup, btnRef, btnHist, btnKey, btnLang string

	if lang == "en" {
		text = fmt.Sprintf("👋 Welcome, %s!\n💰 Balance: 10 000 chars", userName)
		btnAudio = "🎙 Generate Audio"
		btnTemplates = "📂 Audio Templates"
		btnImg = "🎨 Generate Image"
		btnTopup = "💳 Top Up"
		btnRef = "👥 Referral System"
		btnHist = "📜 History"
		btnKey = "🔑 API Keys Management"
		btnLang = "🌐 Switch Language"
	} else {
		text = fmt.Sprintf("👋 Добро пожаловать, %s!\n💰 Баланс: 10 000 символов", userName)
		btnAudio = "🎙 Генерим аудио"
		btnTemplates = "📂 Шаблоны аудио"
		btnImg = "🎨 Генерим картинку"
		btnTopup = "💳 Пополнить баланс"
		btnRef = "👥 Реферальная система"
		btnHist = "📜 История"
		btnKey = "🔑 Управление API ключами"
		btnLang = "🌐 Сменить язык"
	}

	msg := tgbotapi.NewMessage(chatID, text)

	// Новый порядок кнопок:
	// Ряд 1: Аудио | Шаблоны
	// Ряд 2: Картинка | Баланс
	// Ряд 3: Рефералка | История
	// Ряд 4: Ключи
	// Ряд 5: Язык
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(btnAudio, "btn_gen_audio"),
			tgbotapi.NewInlineKeyboardButtonData(btnTemplates, "btn_templates"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(btnImg, "btn_gen_image"),
			tgbotapi.NewInlineKeyboardButtonData(btnTopup, "btn_topup"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(btnRef, "btn_referral"),
			tgbotapi.NewInlineKeyboardButtonData(btnHist, "btn_history"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(btnKey, "btn_api_keys"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(btnLang, "btn_language"),
		),
	)

	msg.ReplyMarkup = keyboard
	bot.Send(msg)
}

// Меню выбора языка
func sendLanguageSelection(bot *tgbotapi.BotAPI, chatID int64, messageID int) {
	text := "🌐 Выберите язык / Choose language:"
	
	// Смотрим текущий язык, чтобы поставить галочку (опционально)
	curr := getSetting(chatID, "lang", "ru")
	ruIcon, enIcon := "", ""
	if curr == "ru" { ruIcon = "✅ " }
	if curr == "en" { enIcon = "✅ " }

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(ruIcon+"🇷🇺 Русский", "set_lang_ru"),
			tgbotapi.NewInlineKeyboardButtonData(enIcon+"🇬🇧 English", "set_lang_en"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔙 Назад / Back", "btn_main_menu"),
		),
	)

	// Редактируем, чтобы было красиво
	bot.Send(tgbotapi.NewEditMessageText(chatID, messageID, text))
	bot.Send(tgbotapi.NewEditMessageReplyMarkup(chatID, messageID, keyboard))
}

// --- ДАЛЕЕ СТАРЫЕ ФУНКЦИИ (БЕЗ ИЗМЕНЕНИЙ В ЛОГИКЕ, ТОЛЬКО КОД) ---

func sendTemplateSettingsMenu(bot *tgbotapi.BotAPI, chatID int64, messageID int, templateName string, notificationType string, newValue string) {
	var header string
	if notificationType == "success" {
		header = fmt.Sprintf("✅ *Параметр обновлен!*\n"+
			"📝 *Новое значение:* %s\n\n", newValue)
	} else {
		header = fmt.Sprintf("🎉 *Редактирование шаблона* %s\n"+
			"Вы можете изменить его параметры ниже ⤵️", templateName)
	}
	text := header

	pauseState := getSetting(chatID, "pause_enabled", "false")
	pauseIcon := "❌"
	if pauseState == "true" {
		pauseIcon = "✅"
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔊 Модель", "tpl_model"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🎚 Тонкая настройка голоса", "tpl_voice_settings"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📦 Формат ответа", "tpl_format"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📏 Лимит фрагмента (Chunk)", "tpl_chunk_size"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(pauseIcon+" Разделитель (Pause)", "tpl_pause_chunk"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⏱ Время тишины", "tpl_pause_len"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔎 Найти голос (Search)", "tpl_search_voice"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⚙️ Управление шаблоном", "tpl_manage"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", "btn_templates"),
		),
	)

	if messageID != 0 {
		bot.Send(tgbotapi.NewEditMessageText(chatID, messageID, text))
		bot.Send(tgbotapi.NewEditMessageReplyMarkup(chatID, messageID, keyboard))
	} else {
		msg := tgbotapi.NewMessage(chatID, text)
		msg.ParseMode = "Markdown"
		msg.ReplyMarkup = keyboard
		bot.Send(msg)
	}
}

func sendChunkSizeSelection(bot *tgbotapi.BotAPI, chatID int64, messageID int) {
	currentSize := getSetting(chatID, "chunk_size", "2000") // 2000 по умолчанию

	text := fmt.Sprintf("⚙️ *Параметр:* 📏 Лимит фрагмента (Chunk)\n\n"+
		"*Текущее значение:* %s\n\n"+
		"*Описание:* _Максимальное количество символов, отправляемых в ElevenLabs за один раз. Влияет на контекст и интонацию._\n\n"+
		"Выберите значение:", currentSize)

	icon := func(val string) string {
		if val == currentSize {
			return "✅ "
		}
		return ""
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(icon("1000")+"1000", "set_chunk_1000"),
			tgbotapi.NewInlineKeyboardButtonData(icon("1100")+"1100", "set_chunk_1100"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(icon("1200")+"1200", "set_chunk_1200"),
			tgbotapi.NewInlineKeyboardButtonData(icon("1300")+"1300", "set_chunk_1300"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(icon("1400")+"1400", "set_chunk_1400"),
			tgbotapi.NewInlineKeyboardButtonData(icon("1500")+"1500", "set_chunk_1500"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(icon("1600")+"1600", "set_chunk_1600"),
			tgbotapi.NewInlineKeyboardButtonData(icon("1700")+"1700", "set_chunk_1700"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(icon("1800")+"1800", "set_chunk_1800"),
			tgbotapi.NewInlineKeyboardButtonData(icon("1900")+"1900", "set_chunk_1900"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(icon("2000")+"2000", "set_chunk_2000"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", "back_to_tpl_settings"),
		),
	)

	bot.Send(tgbotapi.NewEditMessageText(chatID, messageID, text))
	bot.Send(tgbotapi.NewEditMessageReplyMarkup(chatID, messageID, keyboard))
}

func sendTemplateManagementMenu(bot *tgbotapi.BotAPI, chatID int64, messageID int) {
	name := getSetting(chatID, "template_name", "Без имени")
	model := getSetting(chatID, "model", "Multilingual v2")
	voice := getSetting(chatID, "voice_id", "Default Voice")
	
	stab := getSetting(chatID, "stability", "0.5")
	sim := getSetting(chatID, "similarity", "0.75")
	boost := getSetting(chatID, "boost", "true")
	speed := getSetting(chatID, "speed", "1.0")
	style := getSetting(chatID, "style", "0.0")

	chunk := getSetting(chatID, "chunk_size", "2000")
	pauseOn := getSetting(chatID, "pause_enabled", "false")
	pauseLen := getSetting(chatID, "pause_duration", "1")

	boostText := "Вкл"
	if boost == "false" { boostText = "Выкл" }
	
	pauseText := "Вкл"
	if pauseOn == "false" { pauseText = "Выкл" }

	text := fmt.Sprintf("⚙️ *Конфигурация шаблона:* %s\n\n"+
		"🎙 *Голос:*\n"+
		"• Модель: `%s`\n"+
		"• ID Голоса: `%s`\n\n"+
		"🎚 *Параметры:*\n"+
		"▪️ Устойчивость: `%s`\n"+
		"▪️ Точность: `%s`\n"+
		"▪️ Усиление: `%s`\n"+
		"▪️ Темп: `%s`x\n"+
		"▪️ Экспрессия: `%s`\n\n"+
		"✂️ *Генерация:*\n"+
		"▪️ Лимит фрагмента: `%s`\n"+
		"▪️ Разделитель: `%s`\n"+
		"▪️ Время тишины: `%s сек`\n\n"+
		"ℹ️ _Выберите действие:_ ⤵️",
		name, model, voice, stab, sim, boostText, speed, style, chunk, pauseText, pauseLen)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✏️ Изменить имя", "btn_edit_name"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔄 Сброс настроек", "btn_reset_settings"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🗑 Удалить шаблон", "btn_delete_template"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", "back_to_tpl_settings"),
		),
	)

	if messageID != 0 {
		bot.Send(tgbotapi.NewEditMessageText(chatID, messageID, text))
		bot.Send(tgbotapi.NewEditMessageReplyMarkup(chatID, messageID, keyboard))
	} else {
		msg := tgbotapi.NewMessage(chatID, text)
		msg.ParseMode = "Markdown"
		msg.ReplyMarkup = keyboard
		bot.Send(msg)
	}
}

func sendFormatSelection(bot *tgbotapi.BotAPI, chatID int64, messageID int) {
	currentFmt := getSetting(chatID, "format", "single")
	text := fmt.Sprintf("⚙️ *Параметр:* 📦 Формат ответа\n\n"+
		"*Текущий выбор:* %s\n\n"+
		"*Описание:* _Выберите формат выдачи: единым файлом, частями (chunks) или по абзацам._\n\n"+
		"Доступные варианты:", translateFormat(currentFmt))

	icon := func(val string) string {
		if val == currentFmt { return "✅ " }
		return ""
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(icon("single")+"🎧 Единый файл (Full)", "set_fmt_single"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(icon("chunks")+"🧩 Нарезка (Chunks)", "set_fmt_chunk"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(icon("paragraphs")+"¶ По абзацам", "set_fmt_para"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", "back_to_tpl_settings"),
		),
	)
	bot.Send(tgbotapi.NewEditMessageText(chatID, messageID, text))
	bot.Send(tgbotapi.NewEditMessageReplyMarkup(chatID, messageID, keyboard))
}

func sendVoiceSettingsSubmenu(bot *tgbotapi.BotAPI, chatID int64, messageID int) {
	text := "🛠 *Раздел:* 🎙 Тонкая настройка голоса\n\n" +
		"Здесь вы можете изменить параметры звучания."
	
	boostState := getSetting(chatID, "boost", "true")
	boostIcon := "✅"
	if boostState == "false" { boostIcon = "❌" }

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⏩ Темп речи (Speed)", "set_v_speed"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⚖️ Устойчивость (Stability)", "set_v_stability"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🎭 Точность клонирования", "set_v_similarity"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🎨 Экспрессия (Style)", "set_v_style"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(boostIcon+" Усиление голоса", "set_v_boost_toggle"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад в настройки шаблона", "back_to_tpl_settings"),
		),
	)
	bot.Send(tgbotapi.NewEditMessageText(chatID, messageID, text))
	bot.Send(tgbotapi.NewEditMessageReplyMarkup(chatID, messageID, keyboard))
}

func sendTemplatesList(bot *tgbotapi.BotAPI, chatID int64) {
	templateName := getSetting(chatID, "template_name", "")
	
	var text string
	var keyboard tgbotapi.InlineKeyboardMarkup

	if templateName == "" {
		text = "📂 *Ваши шаблоны:*\n\n_Список пуст_"
		keyboard = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("➕ Создать шаблон", "btn_create_template")),
			tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("🏠 В главное меню", "btn_main_menu")),
		)
	} else {
		text = "📂 *Ваши шаблоны:*\n\n" + templateName
		keyboard = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(templateName, "back_to_tpl_settings")),
			tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("➕ Создать шаблон", "btn_create_template")),
			tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("🏠 В главное меню", "btn_main_menu")),
		)
	}

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
}

func sendInputPrompt(bot *tgbotapi.BotAPI, chatID int64, paramName string, currentVal string, description string, rangeVal string) {
	text := fmt.Sprintf("⚙️ *Параметр:* %s\n\n"+
		"🔹 *Текущее значение:* `%s`\n\n"+
		"ℹ️ *Описание:* _%s_\n\n"+
		"📊 *Допустимый диапазон:* `%s`\n\n"+
		"👇 *Введите новое значение:*", paramName, currentVal, description, rangeVal)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("❌ Отменить ввод", "cancel_input")),
	)
	bot.Send(msg)
}

func sendInputNamePrompt(bot *tgbotapi.BotAPI, chatID int64) {
	msg := tgbotapi.NewMessage(chatID, "✏️ *Введите название для нового шаблона:*\n📌 От 3 до 16 символов\n🚫 Без спецсимволов")
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", "btn_templates")),
	)
	bot.Send(msg)
}

func saveSetting(chatID int64, key string, value string) {
	if userSettings[chatID] == nil {
		userSettings[chatID] = make(map[string]string)
	}
	userSettings[chatID][key] = value
}

func getSetting(chatID int64, key string, defaultValue string) string {
	if userSettings[chatID] == nil {
		return defaultValue
	}
	val, ok := userSettings[chatID][key]
	if !ok {
		return defaultValue
	}
	return val
}

func getTemplateName(chatID int64) string {
	return getSetting(chatID, "template_name", "Новый шаблон")
}

func setDefaultSettings(chatID int64, name string) {
	saveSetting(chatID, "template_name", name)
	saveSetting(chatID, "model", "Multilingual v2")
	saveSetting(chatID, "format", "single")
	saveSetting(chatID, "voice_id", "Default Voice")
	saveSetting(chatID, "stability", "0.5")
	saveSetting(chatID, "similarity", "0.75")
	saveSetting(chatID, "style", "0.0")
	saveSetting(chatID, "speed", "1.0")
	saveSetting(chatID, "boost", "true")
	saveSetting(chatID, "chunk_size", "2000")
	saveSetting(chatID, "pause_enabled", "false")
	saveSetting(chatID, "pause_duration", "1")
}

func translateFormat(fmtCode string) string {
	switch fmtCode {
	case "single": return "🎧 Единый файл (Full)"
	case "chunks": return "🧩 Нарезка (Chunks)"
	case "paragraphs": return "¶ По абзацам"
	default: return fmtCode
	}
}

func handleTemplateNameInput(bot *tgbotapi.BotAPI, chatID int64, text string) {
	length := utf8.RuneCountInString(text)
	if length < 3 || length > 16 {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ *Ошибка длины!* (3-16 символов)"))
		return
	}
	match, _ := regexp.MatchString("^[a-zA-Z0-9а-яА-ЯёЁ ]+$", text)
	if !match {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ *Только буквы и цифры!*"))
		return
	}
	tempTemplateData[chatID] = text
	msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("🔍 *Подтвердите создание шаблона:*\n🔥 Название: %s\n✅ Всё верно?", text))
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Создать", "btn_confirm_template_create"),
			tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", "btn_templates"),
		),
	)
	bot.Send(msg)
}

func sendModelSelection(bot *tgbotapi.BotAPI, chatID int64, messageID int) {
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("Eleven v3", "set_model_eleven_v3")),
		tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("Multilingual v2", "set_model_multilingual_v2")),
		tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", "back_to_tpl_settings")),
	)
	bot.Send(tgbotapi.NewEditMessageText(chatID, messageID, "⚙️ *Выберите модель:*"))
	bot.Send(tgbotapi.NewEditMessageReplyMarkup(chatID, messageID, keyboard))
}