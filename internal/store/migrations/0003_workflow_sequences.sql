CREATE TABLE resource_sequences(name TEXT PRIMARY KEY,value INTEGER NOT NULL);
INSERT INTO resource_sequences SELECT 'pantry',COALESCE(MAX(id),0) FROM pantry;
INSERT INTO resource_sequences SELECT 'cooklog',COALESCE(MAX(id),0) FROM cooklog;
INSERT INTO resource_sequences SELECT 'purchases',COALESCE(MAX(id),0) FROM purchases;
INSERT INTO resource_sequences SELECT 'recipes',COALESCE(MAX(id),0) FROM recipes;
INSERT INTO resource_sequences SELECT 'lists',COALESCE(MAX(id),0) FROM lists;
