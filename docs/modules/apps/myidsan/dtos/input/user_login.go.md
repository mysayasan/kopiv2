# Module: apps/myidsan/dtos/input/user_login.go

## Purpose

Defines the myidsan input DTO for user login update payloads.

## Fields

Mirrors `entities.UserLogin`. Includes `mustChangePassword bool` — allows the admin API to set or clear the forced first-login flag on a user record.
