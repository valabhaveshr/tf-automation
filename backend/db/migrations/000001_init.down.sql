-- WS-A: Data Layer — revert initial schema
-- Drops in dependency-safe order (reverse of creation / FK dependents first).

DROP TABLE IF EXISTS naming_counters;
DROP TABLE IF EXISTS naming_templates;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS module_selections;
DROP TABLE IF EXISTS records;
DROP TABLE IF EXISTS customers;
DROP TABLE IF EXISTS module_schemas;
