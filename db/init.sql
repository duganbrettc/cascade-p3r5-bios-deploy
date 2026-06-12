CREATE TABLE users (
    id            SERIAL PRIMARY KEY,
    username      TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    display_name  TEXT,
    bio           TEXT,
    location      TEXT,
    website       TEXT,
    created_at    TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE posts (
    id         SERIAL PRIMARY KEY,
    user_id    INT REFERENCES users(id),
    body       TEXT NOT NULL CHECK (char_length(body) <= 280),
    created_at TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE follows (
    follower_id INT REFERENCES users(id),
    followee_id INT REFERENCES users(id),
    PRIMARY KEY (follower_id, followee_id)
);
