CREATE TABLE brake_devices (
  id                       TEXT PRIMARY KEY,
  gamepad_id               TEXT NOT NULL UNIQUE,
  label                    TEXT NOT NULL,
  accelerator_input_kind   TEXT NOT NULL CHECK (accelerator_input_kind IN ('axis', 'button')),
  accelerator_input_index  INTEGER NOT NULL CHECK (accelerator_input_index >= 0),
  accelerator_rest_value   REAL NOT NULL,
  accelerator_pressed_value REAL NOT NULL,
  brake_input_kind         TEXT NOT NULL CHECK (brake_input_kind IN ('axis', 'button')),
  brake_input_index        INTEGER NOT NULL CHECK (brake_input_index >= 0),
  brake_rest_value         REAL NOT NULL,
  brake_pressed_value      REAL NOT NULL,
  created_at               TEXT NOT NULL,
  updated_at               TEXT NOT NULL
);
