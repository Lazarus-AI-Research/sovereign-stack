-- Logical databases (design.md §17). Runs once on first postgres start.

CREATE DATABASE sovereign_control;
CREATE DATABASE litellm;
CREATE DATABASE workspace;
CREATE DATABASE phoenix;
CREATE DATABASE vectors;

\connect vectors
CREATE EXTENSION IF NOT EXISTS vector;
CREATE SCHEMA IF NOT EXISTS vectors;
