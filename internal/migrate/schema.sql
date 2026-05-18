CREATE TABLE IF NOT EXISTS users (
    id            BIGSERIAL PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    name          TEXT NOT NULL DEFAULT '',
    avatar_url    TEXT,
    is_premium    BOOLEAN NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS mood_entries (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    score        SMALLINT NOT NULL CHECK (score BETWEEN 1 AND 10),
    stress_level SMALLINT NOT NULL DEFAULT 5 CHECK (stress_level BETWEEN 1 AND 10),
    work_hours   NUMERIC(4,1) NOT NULL DEFAULT 0,
    note         TEXT NOT NULL DEFAULT '',
    tags         TEXT[] NOT NULL DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_mood_entries_user_created
    ON mood_entries(user_id, created_at DESC);

-- IMMUTABLE wrapper: TIMESTAMPTZ → DATE in UTC (needed for functional index)
CREATE OR REPLACE FUNCTION ts_to_date(ts TIMESTAMPTZ)
    RETURNS DATE LANGUAGE SQL IMMUTABLE PARALLEL SAFE AS
    'SELECT ($1 AT TIME ZONE ''UTC'')::DATE';

-- One entry per user per calendar day (UTC)
CREATE UNIQUE INDEX IF NOT EXISTS uq_mood_user_day
    ON mood_entries (user_id, ts_to_date(created_at));

-- In-app notifications
CREATE TABLE IF NOT EXISTS notifications (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type       TEXT NOT NULL,   -- mood_reminder | insight_ready | overwork_alert | streak
    title      TEXT NOT NULL,
    body       TEXT NOT NULL,
    is_read    BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_notifications_user_created
    ON notifications(user_id, created_at DESC);

-- Weekly insight report per user (regenerated every Friday)
CREATE TABLE IF NOT EXISTS insight_reports (
    user_id      BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    data         JSONB NOT NULL DEFAULT '{}',
    generated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Tag stress weights: positive = adds stress, negative = reduces stress
CREATE TABLE IF NOT EXISTS tag_stress_weights (
    tag    TEXT PRIMARY KEY,
    weight SMALLINT NOT NULL DEFAULT 0
);

INSERT INTO tag_stress_weights (tag, weight) VALUES
  ('Дедлайн',                  3),
  ('Конфликт',                  3),
  ('Переработка',               2),
  ('Недосып',                   2),
  ('Критика',                   2),
  ('Много задач',               2),
  ('Встреча с руководителем',   2),
  ('Переговоры',                1),
  ('Устал',                     1),
  ('Обратная связь',            0),
  ('Другое',                    0),
  ('Скучно',                   -1),
  ('Хорошая новость',          -1),
  ('Решил сложную задачу',     -1),
  ('Похвала',                  -2),
  ('Командный успех',          -2)
ON CONFLICT (tag) DO NOTHING;

-- Insight rule templates: title/body may contain {tag}, {delta}, {count}, {avg}
CREATE TABLE IF NOT EXISTS insight_templates (
    rule  TEXT PRIMARY KEY,
    icon  TEXT NOT NULL,
    title TEXT NOT NULL,
    body  TEXT NOT NULL,
    tone  TEXT NOT NULL DEFAULT 'neutral'
);

INSERT INTO insight_templates (rule, icon, title, body, tone) VALUES
  ('worst_tag',    '📉', 'Мы заметили: «{tag}» снижает настроение',    'В дни с этим событием настроение в среднем на {delta} балла ниже обычного. Попробуй снизить частоту или подготовиться заранее.',        'warning'),
  ('best_tag',     '📈', '«{tag}» — твой ресурс',                      'В дни с этим событием настроение на {delta} балла выше обычного. Постарайся включать это в расписание чаще.',                              'positive'),
  ('overwork_many','⏱', '{count} дня с переработкой',                  'Мы заметили: среднее рабочее время в эти дни — {avg} ч. Переработки накапливают усталость быстрее, чем кажется.',                          'warning'),
  ('overwork_one', '⏱', 'День с переработкой',                         'Мы заметили: {avg} ч работы в один из дней — следи, чтобы это не стало нормой.',                                                           'warning'),
  ('stress_up',    '😤','Стресс растёт',                               'Мы заметили: уровень стресса вырос на {delta} пункта по сравнению с предыдущим периодом. Стоит обратить на это внимание.',                  'warning'),
  ('stress_down',  '😌','Стресс снижается',                            'Мы заметили: уровень стресса снизился на {delta} пункта — хороший знак. Продолжай в том же духе.',                                        'positive'),
  ('mood_good',    '✨','Настроение в целом хорошее',                   'Средний балл за период — {avg} из 5. Продолжай замечать что работает и сохраняй эти привычки.',                                            'positive'),
  ('mood_bad',     '💙','Сложный период',                              'Средний балл — {avg} из 5. Мы заметили: это сигнал обратить внимание на восстановление и снизить нагрузку там, где возможно.',             'warning'),
  ('regularity',   '📅','{count} из 7 дней с отметкой',               'Чем регулярнее отмечаешь настроение, тем точнее паттерны. Попробуй отмечать каждый вечер — это занимает меньше минуты.',                   'neutral'),
  ('no_data',      '📊','Накапливаем данные',                          'Продолжай отмечать настроение каждый день — паттерны становятся видны от 5 записей.',                                                       'neutral'),
  ('top_tags',     '🔍','Мы заметили: {tags}',                        'Эти события встречались чаще всего. Продолжай отмечать — скоро увидим как они влияют на настроение.',                                          'neutral')
ON CONFLICT (rule) DO NOTHING;

-- Text analysis results (kept for history; AI not yet integrated)
CREATE TABLE IF NOT EXISTS analysis_results (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    input_text TEXT NOT NULL DEFAULT '',
    result     JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_analysis_results_user_created
    ON analysis_results(user_id, created_at DESC);

-- Per-tag advice texts
CREATE TABLE IF NOT EXISTS advice_templates (
    tag  TEXT PRIMARY KEY,
    text TEXT NOT NULL
);

INSERT INTO advice_templates (tag, text) VALUES
  ('Встреча с руководителем', 'Перед встречей запиши 2–3 ключевых момента — это снижает тревогу и делает разговор предметным.'),
  ('Дедлайн',                 'В дни дедлайна разбивай работу на 25-минутные блоки с паузами — это снижает стресс и ускоряет темп.'),
  ('Переработка',             'После переработки тело нуждается в 8+ часах сна. Запланируй ранний отбой сегодня.'),
  ('Конфликт',                'После конфликта дай себе 20 минут до ответа — за это время острота спадает.'),
  ('Недосып',                 'Даже 20-минутный дневной сон восстанавливает концентрацию на 2–3 часа.'),
  ('Критика',                 'Запиши что именно было сказано — это помогает отделить конструктивное от эмоционального.'),
  ('Много задач',             'Выбери три самые важные задачи и начни с них. Остальное — во вторую очередь.'),
  ('Устал',                   '5-минутная прогулка восстанавливает концентрацию лучше, чем кофе.'),
  ('Скучно',                  'Скука — сигнал, что задача слишком проста. Попробуй найти в ней новый угол или сменить обстановку.'),
  ('Похвала',                 'Зафикси этот момент — напиши одно предложение о том, что произошло. Это ресурс для сложных дней.'),
  ('Хорошая новость',         'Хорошие новости стоит отмечать осознанно — дай себе минуту просто порадоваться.'),
  ('Командный успех',         'Поблагодари команду — это укрепляет связи и создаёт позитивную атмосферу.'),
  ('Обратная связь',          'Запиши одну вещь, которую хочешь попробовать изменить по итогам обратной связи.'),
  ('Переговоры',              'После переговоров выпиши итоги и договорённости — пока они свежие.'),
  ('Решил сложную задачу',    'Отметь этот успех — он пополняет уверенность в себе, которая нужна для следующих вызовов.')
ON CONFLICT (tag) DO NOTHING;

-- ─── Subscription / trial ────────────────────────────────────────────────────
-- trial_started_at: set on first Live Response use; NULL = not started yet
ALTER TABLE users ADD COLUMN IF NOT EXISTS trial_started_at TIMESTAMPTZ;
-- lr_used_month + lr_month_key: free-tier counter, reset on new YYYY-MM
ALTER TABLE users ADD COLUMN IF NOT EXISTS lr_used_month  INT  NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS lr_month_key   TEXT NOT NULL DEFAULT '';

-- ─── Weekly summary cards ──────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS weekly_cards (
    user_id      BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    data         JSONB NOT NULL DEFAULT '{}',
    week_start   DATE NOT NULL,
    generated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ─── Live Response ────────────────────────────────────────────────────────────

-- Each AI response session triggered by an event
CREATE TABLE IF NOT EXISTS live_response_sessions (
    id                BIGSERIAL PRIMARY KEY,
    user_id           BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    event_tag         TEXT NOT NULL,
    event_weight      SMALLINT NOT NULL DEFAULT 0,
    user_note         TEXT NOT NULL DEFAULT '',
    ai_message        TEXT NOT NULL DEFAULT '',
    advice_tags       TEXT[] NOT NULL DEFAULT '{}',
    followup_hours    SMALLINT,                        -- NULL means no follow-up scheduled
    followup_question TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_live_sessions_user_created
    ON live_response_sessions(user_id, created_at DESC);

-- Prevents two concurrent Generate calls for the same user+event on the same UTC day
-- from both persisting separate sessions (race condition when first request times out).
CREATE UNIQUE INDEX IF NOT EXISTS uq_live_session_user_tag_day
    ON live_response_sessions(user_id, event_tag, ts_to_date(created_at));

-- Simple helpful / not-helpful feedback per session
CREATE TABLE IF NOT EXISTS live_response_feedback (
    id         BIGSERIAL PRIMARY KEY,
    session_id BIGINT NOT NULL REFERENCES live_response_sessions(id) ON DELETE CASCADE UNIQUE,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    is_helpful BOOLEAN NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Scheduled follow-up push notifications
CREATE TABLE IF NOT EXISTS followups (
    id           BIGSERIAL PRIMARY KEY,
    session_id   BIGINT NOT NULL REFERENCES live_response_sessions(id) ON DELETE CASCADE,
    user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    scheduled_at TIMESTAMPTZ NOT NULL,
    status       TEXT NOT NULL DEFAULT 'pending'
                     CHECK (status IN ('pending', 'answered_yes', 'answered_no', 'skipped', 'declined')),
    answered_at  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Widen followups.status to include 'declined' (consent UI refusal) on existing DBs
ALTER TABLE followups
    DROP CONSTRAINT IF EXISTS followups_status_check,
    ADD CONSTRAINT followups_status_check
        CHECK (status IN ('pending', 'answered_yes', 'answered_no', 'skipped', 'declined'));

CREATE INDEX IF NOT EXISTS idx_followups_user_scheduled
    ON followups(user_id, scheduled_at);
-- Partial index: only pending rows are queried by the cron job
CREATE INDEX IF NOT EXISTS idx_followups_pending
    ON followups(scheduled_at) WHERE status = 'pending';

-- Aggregated advice outcomes — source of truth for personalisation
-- next_day_mood is NULL until the nightly cron job fills it from mood_entries
CREATE TABLE IF NOT EXISTS advice_outcomes (
    id              BIGSERIAL PRIMARY KEY,
    user_id         BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    session_id      BIGINT NOT NULL REFERENCES live_response_sessions(id) ON DELETE CASCADE,
    advice_tag      TEXT NOT NULL,
    event_tag       TEXT NOT NULL,
    followup_answer TEXT CHECK (followup_answer IN ('yes', 'no', 'skipped', 'declined')),
    next_day_mood   SMALLINT CHECK (next_day_mood BETWEEN 1 AND 10),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Widen advice_outcomes.followup_answer to include 'declined' on existing DBs
ALTER TABLE advice_outcomes
    DROP CONSTRAINT IF EXISTS advice_outcomes_followup_answer_check,
    ADD CONSTRAINT advice_outcomes_followup_answer_check
        CHECK (followup_answer IN ('yes', 'no', 'skipped', 'declined'));

CREATE INDEX IF NOT EXISTS idx_advice_outcomes_user_tag
    ON advice_outcomes(user_id, advice_tag);

-- Chat messages within a live response session (multi-turn conversation)
CREATE TABLE IF NOT EXISTS live_response_messages (
    id         BIGSERIAL PRIMARY KEY,
    session_id BIGINT NOT NULL REFERENCES live_response_sessions(id) ON DELETE CASCADE,
    role       TEXT NOT NULL CHECK (role IN ('user', 'assistant')),
    content    TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_lr_messages_session
    ON live_response_messages(session_id, created_at);

-- ─── Event types catalogue ─────────────────────────────────────────────────────
-- Single source of truth for all loggable events.
-- response_type: 'instant' = multi-turn chat, 'advice' = one-shot card, 'nothing' = log only.
-- chips: list of quick-select clarification options shown in the UI.
-- opener: first-turn prompt shown above the chat input for instant events.
CREATE TABLE IF NOT EXISTS event_types (
    name           TEXT PRIMARY KEY,
    emoji          TEXT NOT NULL DEFAULT '',
    response_type  TEXT NOT NULL DEFAULT 'advice'
                       CHECK (response_type IN ('instant', 'advice', 'nothing')),
    weight         SMALLINT NOT NULL DEFAULT 1,
    followup_hours SMALLINT NOT NULL DEFAULT 0,
    chips          TEXT[] NOT NULL DEFAULT '{}',
    opener         TEXT NOT NULL DEFAULT '',
    sort_order     INT NOT NULL DEFAULT 100
);

INSERT INTO event_types (name, emoji, response_type, weight, followup_hours, chips, opener, sort_order) VALUES
  -- ── Instant: critical negative ──────────────────────────────────────────────
  ('Конфликт',         '🤬', 'instant', 4, 1,
   ARRAY['С коллегой','С руководителем','С клиентом','С командой'],
   'Расскажи — что произошло?', 10),

  ('Критика',          '😔', 'instant', 4, 1,
   ARRAY['От руководителя','От коллег','Публичная','Несправедливая'],
   'Что именно было сказано?', 20),

  ('Тревога',          '😰', 'instant', 4, 1,
   ARRAY['По работе','По задаче','Не знаю почему','Перед встречей'],
   'Что тебя тревожит прямо сейчас?', 30),

  ('Дедлайн',          '⏰', 'instant', 4, 2,
   ARRAY['Горящий — сегодня','На этой неделе','Срывается','Внезапный'],
   'Расскажи — что не успеваешь?', 40),

  -- ── Instant: новые события ──────────────────────────────────────────────────
  ('Выгораю',          '🔥', 'instant', 4, 2,
   ARRAY['Нет мотивации','Всё раздражает','Чувствую пустоту','Не вижу смысла'],
   'Что сейчас даётся тяжелее всего?', 50),

  ('Некомфортно',      '😬', 'instant', 3, 2,
   ARRAY['В общении','В команде','На встрече','Не понимаю почему'],
   'Что именно ощущаешь?', 60),

  ('Сложный разговор', '💬', 'instant', 3, 2,
   ARRAY['С руководителем','С коллегой','С клиентом','Ещё предстоит'],
   'Расскажи, что произошло?', 70),

  ('Облажался',        '🤦', 'instant', 3, 2,
   ARRAY['По задаче','Перед командой','Перед клиентом','Сам накосячил'],
   'Расскажи, что случилось?', 80),

  -- ── Instant: позитивные ─────────────────────────────────────────────────────
  ('Решил сложную задачу', '💡', 'instant', 2, 2,
   ARRAY['Сам разобрался','Нашёл оригинальное решение','Научился новому','Помогла команда'],
   'Поделись — что это было?', 90),

  ('Хорошая новость',  '🎉', 'instant', 2, 2,
   ARRAY['По проекту','По карьере','По команде','Личная'],
   'Что случилось?', 100),

  -- ── Advice: высокая нагрузка ────────────────────────────────────────────────
  ('Переработка',      '⏱', 'advice', 3, 8,
   ARRAY['Задержался допоздна','Работал на выходных','Не мог остановиться','Задачи не кончаются'],
   '', 110),

  ('Недосып',          '😴', 'advice', 3, 6,
   ARRAY['Меньше 6 часов','Не мог уснуть','Разбудили','Поздно лёг'],
   '', 120),

  ('Много задач',      '📋', 'advice', 3, 4,
   ARRAY['Всё срочное','Не знаю с чего начать','Постоянно отвлекают','Не успеваю'],
   '', 130),

  ('Перегружен',       '🌊', 'advice', 3, 4,
   ARRAY['Задачами','Информацией','Общением','Всем сразу'],
   '', 140),

  -- ── Advice: средняя нагрузка ────────────────────────────────────────────────
  ('Нет сил',          '🪫', 'advice', 2, 4,
   ARRAY['Физически','Морально','После совещаний','К концу дня'],
   '', 150),

  ('Устал',            '😩', 'advice', 2, 4,
   ARRAY['От задач','От людей','От совещаний','Просто устал'],
   '', 160),

  ('Встреча с руководителем', '👔', 'advice', 2, 3,
   ARRAY['Ожидаемая','Неожиданная','Оценка работы','Сложная тема'],
   '', 170),

  ('Переговоры',       '🤝', 'advice', 2, 3,
   ARRAY['Внутренние','С клиентом','Сложные','Ещё предстоят'],
   '', 180),

  ('Не могу начать',   '🔄', 'advice', 2, 3,
   ARRAY['Откладываю задачу','Не знаю с чего начать','Мешает тревога','Нет настроения'],
   '', 190),

  -- ── Advice: низкая нагрузка ─────────────────────────────────────────────────
  ('Обратная связь',   '💬', 'advice', 1, 6,
   ARRAY['Позитивная','Критическая','От руководителя','От команды'],
   '', 200),

  ('Скучно',           '😑', 'advice', 1, 12,
   ARRAY['На работе','На встречах','Нет интересных задач','Монотонно'],
   '', 210),

  ('Другое',           '❓', 'instant', 1, 6,
   ARRAY[]::TEXT[],
   'Расскажи, что произошло?', 220),

  -- ── Nothing: просто логируем ────────────────────────────────────────────────
  ('Все как обычно',   '😐', 'nothing', 0, 0,  ARRAY[]::TEXT[], '', 230),
  ('Похвала',          '🏅', 'nothing', 3, 0,
   ARRAY['От руководителя','От коллег','Публичная','За проект'],
   '', 240),
  ('Командный успех',  '🏆', 'nothing', 3, 0,
   ARRAY['Завершили проект','Важная победа','Хвалили команду','Закрыли квартал'],
   '', 250),
  ('Хороший день',     '🌟', 'nothing', 0, 0,  ARRAY[]::TEXT[], '', 260),
  ('Сделал важное',    '✅', 'nothing', 1, 0,
   ARRAY['Закончил задачу','Принял решение','Важный шаг','Закрыл проект'],
   '', 270)
ON CONFLICT (name) DO NOTHING;
