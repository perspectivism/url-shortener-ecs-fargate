# 🏗️ URL Shortener — System Design

> **Status:** skeleton (M0). Completed in M9 following the interview walkthrough
> order (SPEC §17.5). Sections are filled in as each milestone lands.

## Contents

1. [Requirements](#1-requirements)
2. [Architecture](#2-architecture)
3. [Deployment](#3-deployment)

## 1. Requirements

See SPEC §2. To be expanded in M9.

## 2. Architecture

```mermaid
flowchart LR
    client(["🧑‍💻 Client"]) --> edge["⚡ CloudFront + WAF"]
    edge --> app["🐳 ECS Fargate services"]
    app --> data[("🗂️ DynamoDB · ⚡ Valkey")]
```

## 3. Deployment

Written in M5.
