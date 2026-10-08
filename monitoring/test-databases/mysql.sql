-- Isolated test containers only. The observer has no write or administrative grant.
CREATE DATABASE IF NOT EXISTS pulse_test;
CREATE USER IF NOT EXISTS 'pulse_observer'@'%' IDENTIFIED BY 'pulse_test_only';
GRANT SELECT ON pulse_test.* TO 'pulse_observer'@'%';
