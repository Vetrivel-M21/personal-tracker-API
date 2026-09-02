ALTER TABLE todos ADD COLUMN status text NOT NULL DEFAULT 'todo' CHECK (status IN ('todo', 'in_progress', 'done'));
ALTER TABLE todos ADD COLUMN priority text NOT NULL DEFAULT 'medium' CHECK (priority IN ('low', 'medium', 'high'));

UPDATE todos SET status = 'done' WHERE completed;

ALTER TABLE todos DROP COLUMN completed;
