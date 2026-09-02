ALTER TABLE todos ADD COLUMN completed boolean NOT NULL DEFAULT false;

UPDATE todos SET completed = (status = 'done');

ALTER TABLE todos DROP COLUMN priority;
ALTER TABLE todos DROP COLUMN status;
