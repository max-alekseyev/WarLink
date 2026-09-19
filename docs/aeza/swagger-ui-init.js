Title: Live Content

Description: Fetched live

Source: https://my.aeza.net/api/v2/docs/swagger-ui-init.js

---


window.onload = function() {
  // Build a system
  let url = window.location.search.match(/url=([^&]+)/);
  if (url && url.length > 1) {
    url = decodeURIComponent(url[1]);
  } else {
    url = window.location.origin;
  }
  let options = {
  "swaggerDoc": {
    "openapi": "3.0.0",
    "paths": {
      "/domains/check": {
        "post": {
          "operationId": "checkDomainAvailable",
          "parameters": [],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/CheckDomainAvailableRequestDto"
                }
              }
            }
          },
          "responses": {
            "200": {
              "description": "Domain availability check results for all zones",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/CheckDomainAvailableResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "Check domain availability",
          "tags": [
            "domains"
          ]
        }
      },
      "/services/products": {
        "get": {
          "operationId": "getList",
          "parameters": [
            {
              "name": "type",
              "required": false,
              "in": "query",
              "schema": {
                "type": "string"
              }
            },
            {
              "name": "group",
              "required": false,
              "in": "query",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/ProductsResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            },
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "get service products list",
          "tags": [
            "service-products"
          ]
        }
      },
      "/services/products/parameters-schema": {
        "get": {
          "description": "Returns a list of product-determining parameters of the specified PM module ",
          "operationId": "getPmParametersSchemaClient",
          "parameters": [
            {
              "name": "pm",
              "required": true,
              "in": "query",
              "schema": {
                "type": "string"
              }
            }
          ],
          "responses": {
            "200": {
              "description": ""
            },
            "400": {
              "description": "PM module does not support parameters"
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "Get PM module parameters schema (client view)",
          "tags": [
            "service-products"
          ]
        }
      },
      "/services/products/commands-schema": {
        "get": {
          "description": "Returns a list of commands supported by the specified PM module",
          "operationId": "getPmCommandsSchemaClient",
          "parameters": [
            {
              "name": "pm",
              "required": true,
              "in": "query",
              "schema": {
                "type": "string"
              }
            }
          ],
          "responses": {
            "200": {
              "description": ""
            },
            "400": {
              "description": "PM module does not support commands"
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "Get PM module commands schema (client view)",
          "tags": [
            "service-products"
          ]
        }
      },
      "/services/groups": {
        "get": {
          "operationId": "getList",
          "parameters": [
            {
              "name": "type",
              "required": false,
              "in": "query",
              "schema": {
                "type": "string"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/ProductGroupsResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "get service product groups list",
          "tags": [
            "service-product-groups"
          ]
        }
      },
      "/centrifugo/auth": {
        "get": {
          "operationId": "getAuthToken",
          "parameters": [],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/CentrifugoAuthTokenResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "get centrifugo auth token",
          "tags": [
            "centrifugo"
          ]
        }
      },
      "/centrifugo/auth/legacy-support": {
        "get": {
          "operationId": "getChannelLegacySupportToken",
          "parameters": [],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/CentrifugoAuthTokenResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "get legacy support channel token",
          "tags": [
            "centrifugo"
          ]
        }
      },
      "/accounts/me/password": {
        "put": {
          "operationId": "AccountsController_changePassword",
          "parameters": [],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/ChangePasswordRequestDto"
                }
              }
            }
          },
          "responses": {
            "204": {
              "description": "password changed"
            },
            "400": {
              "description": "invalid_data"
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "change password",
          "tags": [
            "accounts"
          ]
        }
      },
      "/accounts/me": {
        "get": {
          "operationId": "AccountsController_getInfo",
          "parameters": [],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/AccountResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "get account info",
          "tags": [
            "accounts"
          ]
        },
        "patch": {
          "operationId": "patchAccountMe",
          "parameters": [],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/PatchAccountMeRequestDto"
                }
              }
            }
          },
          "responses": {
            "204": {
              "description": "account data updated"
            },
            "400": {
              "description": "invalid_data"
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "Partially update current account (interface lang/theme, profile; legal only if billing region is RU). Currency is not changed here.",
          "tags": [
            "accounts"
          ]
        }
      },
      "/accounts/me/phone": {
        "put": {
          "operationId": "AccountsController_changePhone",
          "parameters": [],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/ChangePhoneRequestDto"
                }
              }
            }
          },
          "responses": {
            "200": {
              "description": "number to call for verification",
              "content": {
                "application/json": {
                  "schema": {
                    "type": "string"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "initiate phone change",
          "tags": [
            "accounts"
          ]
        }
      },
      "/accounts/me/phone/confirm": {
        "post": {
          "operationId": "AccountsController_confirmPhone",
          "parameters": [],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/ConfirmPhoneRequestDto"
                }
              }
            }
          },
          "responses": {
            "204": {
              "description": "new phone saved"
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "confirm change phone",
          "tags": [
            "accounts"
          ]
        }
      },
      "/referral-programs": {
        "get": {
          "operationId": "getDefault",
          "parameters": [],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/ReferralProgramsItemResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "Get default referral program",
          "tags": [
            "referral-programs"
          ]
        }
      },
      "/referral-programs/top": {
        "get": {
          "operationId": "getTop",
          "parameters": [],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/ReferralProgramsTopResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "Get top referral programs",
          "tags": [
            "referral-programs"
          ]
        }
      },
      "/referral-programs/history": {
        "get": {
          "operationId": "getHistory",
          "parameters": [
            {
              "name": "offset",
              "required": false,
              "in": "query",
              "schema": {
                "default": 0,
                "type": "number"
              }
            },
            {
              "name": "limit",
              "required": false,
              "in": "query",
              "schema": {
                "default": 50,
                "type": "number"
              }
            },
            {
              "name": "sort",
              "required": false,
              "in": "query",
              "schema": {
                "type": "string"
              }
            },
            {
              "name": "filter",
              "required": false,
              "in": "query",
              "schema": {
                "type": "string"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/ReferralProgramsHistoryResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "Get referral program operations history",
          "tags": [
            "referral-programs"
          ]
        }
      },
      "/referral-programs/tiers": {
        "get": {
          "operationId": "getTiers",
          "parameters": [],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/ReferralTiersResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "Get referral tiers",
          "tags": [
            "referral-programs"
          ]
        }
      },
      "/withdraws": {
        "post": {
          "operationId": "WithdrawsController_init",
          "parameters": [],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/InitWithdrawRequestDto"
                }
              }
            }
          },
          "responses": {
            "201": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/WithdrawListResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "init new withdraw",
          "tags": [
            "withdraws"
          ]
        },
        "get": {
          "operationId": "WithdrawsController_getList",
          "parameters": [
            {
              "name": "offset",
              "required": false,
              "in": "query",
              "schema": {
                "default": 0,
                "type": "number"
              }
            },
            {
              "name": "limit",
              "required": false,
              "in": "query",
              "schema": {
                "default": 50,
                "type": "number"
              }
            }
          ],
          "responses": {
            "201": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/WithdrawsResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "get withdraws",
          "tags": [
            "withdraws"
          ]
        }
      },
      "/withdraws/methods": {
        "get": {
          "operationId": "WithdrawsController_getMethods",
          "parameters": [],
          "responses": {
            "201": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "type": "array",
                    "items": {
                      "$ref": "#/components/schemas/WithdrawMethodResponseDto"
                    }
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "get withdraw methods",
          "tags": [
            "withdraws"
          ]
        }
      },
      "/withdraws/limits": {
        "get": {
          "operationId": "WithdrawsController_getLimits",
          "parameters": [],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/WithdrawLimitsResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "get withdraw limits",
          "tags": [
            "withdraws"
          ]
        }
      },
      "/files": {
        "post": {
          "operationId": "FilesController_createLink",
          "parameters": [],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/CreateFileRequestDto"
                }
              }
            }
          },
          "responses": {
            "201": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/FilePresignedUrlResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "create presigned url to upload file",
          "tags": [
            "files"
          ]
        }
      },
      "/files/save": {
        "post": {
          "operationId": "FilesController_save",
          "parameters": [],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/SaveFileRequestDto"
                }
              }
            }
          },
          "responses": {
            "201": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/FileResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "save uploaded file",
          "tags": [
            "files"
          ]
        }
      },
      "/feature-flags": {
        "get": {
          "operationId": "getAllFeatureFlags",
          "parameters": [],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "type": "array",
                    "items": {
                      "type": "string"
                    }
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "Get all user feature flags",
          "tags": [
            "feature-flags"
          ]
        }
      },
      "/sessions": {
        "get": {
          "operationId": "SessionsController_getActive",
          "parameters": [],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/SessionsResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "get active sessions",
          "tags": [
            "sessions"
          ]
        }
      },
      "/sessions/other": {
        "delete": {
          "operationId": "SessionsController_invalidateOther",
          "parameters": [],
          "responses": {
            "204": {
              "description": "sessions deleted"
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "close all other sessions",
          "tags": [
            "sessions"
          ]
        }
      },
      "/sessions/{id}": {
        "delete": {
          "operationId": "SessionsController_invalidateById",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "204": {
              "description": "session deleted"
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "close session by id",
          "tags": [
            "sessions"
          ]
        }
      },
      "/auth/tfa": {
        "get": {
          "operationId": "AuthTfaController_getAvailableMethods",
          "parameters": [
            {
              "name": "challenge",
              "required": true,
              "in": "query",
              "schema": {
                "type": "string"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "totp, backup_code, challenge_allow",
              "content": {
                "application/json": {
                  "schema": {
                    "type": "array",
                    "items": {
                      "type": "string"
                    }
                  }
                }
              }
            },
            "400": {
              "description": "invalid_challenge"
            },
            "403": {
              "description": "tfa_attempts_exceeded"
            }
          },
          "summary": "get available methods",
          "tags": [
            "auth"
          ]
        }
      },
      "/auth/tfa/backup-code": {
        "post": {
          "operationId": "AuthTfaController_useBackupCode",
          "parameters": [],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/UseBackupCodeRequestDto"
                }
              }
            }
          },
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/AuthChallengeResponseDto"
                  }
                }
              }
            },
            "400": {
              "description": "invalid_challenge"
            },
            "403": {
              "description": "tfa_attempts_exceeded"
            }
          },
          "summary": "use backup code",
          "tags": [
            "auth"
          ]
        }
      },
      "/auth/tfa/totp": {
        "post": {
          "operationId": "AuthTfaController_useTotp",
          "parameters": [],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/UseTotpRequestDto"
                }
              }
            }
          },
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/AuthChallengeResponseDto"
                  }
                }
              }
            },
            "400": {
              "description": "invalid_challenge"
            },
            "403": {
              "description": "tfa_attempts_exceeded"
            }
          },
          "summary": "use TOTP method",
          "tags": [
            "auth"
          ]
        }
      },
      "/auth/tfa/wait-allow": {
        "post": {
          "operationId": "AuthTfaController_waitAllow",
          "parameters": [],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/UseChallengeRequestDto"
                }
              }
            }
          },
          "responses": {
            "200": {
              "description": ""
            },
            "400": {
              "description": "invalid_challenge"
            },
            "403": {
              "description": "tfa_attempts_exceeded"
            }
          },
          "summary": "longpool waiting for remote login to be allowed",
          "tags": [
            "auth"
          ]
        }
      },
      "/auth/tfa/check-allow": {
        "post": {
          "operationId": "AuthTfaController_checkAllow",
          "parameters": [],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/UseChallengeRequestDto"
                }
              }
            }
          },
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/AuthChallengeResponseDto"
                  }
                }
              }
            },
            "400": {
              "description": "invalid_challenge"
            },
            "403": {
              "description": "tfa_attempts_exceeded"
            }
          },
          "summary": "get challenge",
          "tags": [
            "auth"
          ]
        }
      },
      "/consent": {
        "post": {
          "operationId": "ConsentsController_create",
          "parameters": [],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/CreateConsentsDto"
                }
              }
            }
          },
          "responses": {
            "201": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/ConsentCreatedResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "create or update consents",
          "tags": [
            "consents"
          ]
        }
      },
      "/consent/me": {
        "get": {
          "operationId": "ConsentsController_getMyConsents",
          "parameters": [],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "type": "array",
                    "items": {
                      "$ref": "#/components/schemas/AccountConsentsResponseDto"
                    }
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "get current account consents",
          "tags": [
            "consents"
          ]
        }
      },
      "/consent/revoke": {
        "post": {
          "operationId": "ConsentsController_revoke",
          "parameters": [],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/RevokeConsentDto"
                }
              }
            }
          },
          "responses": {
            "204": {
              "description": "consent revoked"
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "revoke marketing consent",
          "tags": [
            "consents"
          ]
        }
      },
      "/account/payment-methods/{id}": {
        "get": {
          "operationId": "AccountPaymentMethodsController_getById",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "string"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/AccountPaymentMethodResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "Get account payment method by id",
          "tags": [
            "account-payment-methods"
          ]
        },
        "delete": {
          "operationId": "AccountPaymentMethodsController_delete",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "string"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "Account payment method successfully deleted"
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "Delete account payment method",
          "tags": [
            "account-payment-methods"
          ]
        }
      },
      "/account/payment-methods": {
        "get": {
          "operationId": "AccountPaymentMethodsController_getList",
          "parameters": [
            {
              "name": "offset",
              "required": false,
              "in": "query",
              "schema": {
                "default": 0,
                "type": "number"
              }
            },
            {
              "name": "limit",
              "required": false,
              "in": "query",
              "schema": {
                "default": 50,
                "type": "number"
              }
            },
            {
              "name": "sort",
              "required": false,
              "in": "query",
              "schema": {
                "type": "string"
              }
            },
            {
              "name": "filter",
              "required": false,
              "in": "query",
              "schema": {
                "type": "string"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "Account payment methods list",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/AccountPaymentMethodsListResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "Get account payment methods",
          "tags": [
            "account-payment-methods"
          ]
        },
        "post": {
          "operationId": "AccountPaymentMethodsController_create",
          "parameters": [],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/CreatePaymentMethodRequestDto"
                }
              }
            }
          },
          "responses": {
            "200": {
              "description": "Account payment method successfully created",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/CreateAccountPaymentMethodResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "Create account payment method",
          "tags": [
            "account-payment-methods"
          ]
        }
      },
      "/billing/payment-methods": {
        "get": {
          "description": "Returns list of payment methods available for the authenticated user based on their region and account type",
          "operationId": "PaymentMethodsController_getList",
          "parameters": [],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/PaymentMethodsListResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "Get available payment methods",
          "tags": [
            "billing-payment-methods"
          ]
        }
      },
      "/billing/payment-methods/{method}/exchange-rates": {
        "get": {
          "operationId": "PaymentMethodsController_getExchangeRates",
          "parameters": [
            {
              "name": "method",
              "required": true,
              "in": "path",
              "schema": {
                "type": "string"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/PaymentMethodExchangeRatesListResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "tags": [
            "billing-payment-methods"
          ]
        }
      },
      "/api-keys": {
        "post": {
          "operationId": "create",
          "parameters": [],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/EditApiKeyDataRequestDto"
                }
              }
            }
          },
          "responses": {
            "201": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/ApiKeyCreatedResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "Create a new API key",
          "tags": [
            "api-keys"
          ]
        },
        "get": {
          "operationId": "getList",
          "parameters": [],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/ApiKeyListResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "Get all API keys",
          "tags": [
            "api-keys"
          ]
        }
      },
      "/api-keys/{id}": {
        "patch": {
          "operationId": "ApiKeysController_edit",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/EditApiKeyDataRequestDto"
                }
              }
            }
          },
          "responses": {
            "204": {
              "description": "new api key data saved"
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "edit your existing api key data",
          "tags": [
            "api-keys"
          ]
        },
        "delete": {
          "operationId": "delete",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "204": {
              "description": "API key deleted"
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "Delete an API key by id",
          "tags": [
            "api-keys"
          ]
        }
      },
      "/auth/sso": {
        "post": {
          "operationId": "AuthSsoController_init",
          "parameters": [],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/SsoInitRequestDto"
                }
              }
            }
          },
          "responses": {
            "201": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/SsoInitResponseDto"
                  }
                }
              }
            }
          },
          "summary": "init sso action",
          "tags": [
            "auth"
          ]
        }
      },
      "/auth/sso/login": {
        "post": {
          "operationId": "AuthSsoController_confirmLogin",
          "parameters": [],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/SsoConfirmRequestDto"
                }
              }
            }
          },
          "responses": {
            "201": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/AuthChallengeResponseDto"
                  }
                }
              }
            },
            "400": {
              "description": "invalid_data, sso_not_linked"
            }
          },
          "summary": "login using sso",
          "tags": [
            "auth"
          ]
        }
      },
      "/auth/sso/register": {
        "post": {
          "operationId": "AuthSsoController_confirmRegister",
          "parameters": [],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/SsoConfirmRegisterRequestDto"
                }
              }
            }
          },
          "responses": {
            "201": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/AuthChallengeResponseDto"
                  }
                }
              }
            },
            "400": {
              "description": "invalid_data, sso_link_exists"
            }
          },
          "summary": "register using sso",
          "tags": [
            "auth"
          ]
        }
      },
      "/auth/sso/available": {
        "get": {
          "operationId": "AuthSsoController_getAvaliable",
          "parameters": [],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "type": "array",
                    "items": {
                      "type": "string"
                    }
                  }
                }
              }
            }
          },
          "summary": "get available providers",
          "tags": [
            "auth"
          ]
        }
      },
      "/sso": {
        "get": {
          "operationId": "SsoController_getList",
          "parameters": [],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/SsoLinksResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "get sso links",
          "tags": [
            "sso"
          ]
        },
        "post": {
          "operationId": "SsoController_link",
          "parameters": [],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/SsoConfirmRequestDto"
                }
              }
            }
          },
          "responses": {
            "204": {
              "description": "sso service connected to account"
            },
            "400": {
              "description": "invalid_data, sso_link_exists"
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "link sso-method to account",
          "tags": [
            "sso"
          ]
        }
      },
      "/sso/{providerType}": {
        "delete": {
          "operationId": "SsoController_delete",
          "parameters": [
            {
              "name": "providerType",
              "required": true,
              "in": "path",
              "schema": {
                "type": "string",
                "enum": [
                  "dev",
                  "yandex",
                  "google",
                  "github",
                  "apple",
                  "telegram",
                  "discord"
                ]
              }
            }
          ],
          "responses": {
            "204": {
              "description": ""
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "unlink sso-method from account",
          "tags": [
            "sso"
          ]
        }
      },
      "/oauth/clients/{clientId}": {
        "get": {
          "operationId": "getOAuthClientInfo",
          "parameters": [
            {
              "name": "clientId",
              "required": true,
              "in": "path",
              "schema": {
                "type": "string"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/OAuthClientInfoResponseDto"
                  }
                }
              }
            }
          },
          "summary": "Get OAuth client information",
          "tags": [
            "oauth"
          ]
        }
      },
      "/oauth/authorize": {
        "post": {
          "operationId": "authorizeOAuth",
          "parameters": [],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/OAuthAuthorizeRequestDto"
                }
              }
            }
          },
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/OAuthAuthorizeResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "Authorize OAuth client",
          "tags": [
            "oauth"
          ]
        }
      },
      "/notifications": {
        "get": {
          "operationId": "getList",
          "parameters": [
            {
              "name": "offset",
              "required": false,
              "in": "query",
              "schema": {
                "default": 0,
                "type": "number"
              }
            },
            {
              "name": "limit",
              "required": false,
              "in": "query",
              "schema": {
                "default": 50,
                "type": "number"
              }
            },
            {
              "name": "sort",
              "required": false,
              "in": "query",
              "schema": {
                "type": "string"
              }
            },
            {
              "name": "filter",
              "required": false,
              "in": "query",
              "schema": {
                "type": "string"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/NotificationListResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "Get list of user notifications",
          "tags": [
            "notifications"
          ]
        }
      },
      "/notifications/{id}": {
        "get": {
          "operationId": "getOne",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "description": "Notification entry ID",
              "schema": {
                "example": 123,
                "type": "number"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/NotificationResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "Get notification by ID",
          "tags": [
            "notifications"
          ]
        }
      },
      "/notifications/read": {
        "post": {
          "operationId": "markAllAsRead",
          "parameters": [],
          "responses": {
            "204": {
              "description": "All notifications marked as read"
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "Mark all notifications as read",
          "tags": [
            "notifications"
          ]
        }
      },
      "/notifications/{id}/read": {
        "post": {
          "operationId": "markAsRead",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "description": "Notification entry ID",
              "schema": {
                "example": 123,
                "type": "number"
              }
            }
          ],
          "responses": {
            "204": {
              "description": "Notification marked as read"
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "Mark notification as read",
          "tags": [
            "notifications"
          ]
        }
      },
      "/support/tickets": {
        "get": {
          "operationId": "SupportController_getList",
          "parameters": [],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/TicketsResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "get ticket list",
          "tags": [
            "support"
          ]
        },
        "post": {
          "operationId": "SupportController_create",
          "parameters": [],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/CreateTicketRequestDto"
                }
              }
            }
          },
          "responses": {
            "201": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/TicketResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "create ticket",
          "tags": [
            "support"
          ]
        }
      },
      "/support/tickets/count": {
        "get": {
          "operationId": "SupportController_getOpenedTicketCount",
          "parameters": [],
          "responses": {
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            },
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "get opened ticket count",
          "tags": [
            "support"
          ]
        }
      },
      "/support/tickets/{id}": {
        "get": {
          "operationId": "SupportController_getOne",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/TicketResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "get ticket info",
          "tags": [
            "support"
          ]
        }
      },
      "/support/tickets/{id}/archive": {
        "post": {
          "operationId": "SupportController_archive",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "204": {
              "description": "ticket is solved"
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "mark ticket as solved",
          "tags": [
            "support"
          ]
        }
      },
      "/support/tickets/{id}/messages": {
        "get": {
          "operationId": "SupportController_getMessages",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/TicketMessagesResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "get ticket messages",
          "tags": [
            "support"
          ]
        },
        "post": {
          "operationId": "SupportController_sendMessage",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/SendMessageRequestDto"
                }
              }
            }
          },
          "responses": {
            "201": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/TicketMessageListResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "send message to ticket",
          "tags": [
            "support"
          ]
        }
      },
      "/support/tickets/{id}/read": {
        "post": {
          "operationId": "SupportController_read",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "204": {
              "description": "the ticket has been read"
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "mark ticket as read",
          "tags": [
            "support"
          ]
        }
      },
      "/support/tickets/{id}/messages/{messageId}/reaction": {
        "patch": {
          "operationId": "SupportController_setReaction",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            },
            {
              "name": "messageId",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/SetMessageReactionRequestDto"
                }
              }
            }
          },
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/TicketMessageListResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "set reaction to support message",
          "tags": [
            "support"
          ]
        }
      },
      "/support/tickets/{id}/rate": {
        "get": {
          "operationId": "SupportRatesController_get",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/TicketRateResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "get ticket rate info",
          "tags": [
            "support-rates"
          ]
        },
        "post": {
          "operationId": "SupportRatesController_rate",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/TicketRateRequestDto"
                }
              }
            }
          },
          "responses": {
            "201": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/TicketRateResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "rate ticket",
          "tags": [
            "support-rates"
          ]
        }
      },
      "/support/tickets/{id}/rate/tips": {
        "get": {
          "operationId": "SupportRatesController_getTips",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "201": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/TicketTipsResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "get ticket tips",
          "tags": [
            "support-rates"
          ]
        },
        "post": {
          "operationId": "SupportRatesController_tip",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/TicketTipRequestDto"
                }
              }
            }
          },
          "responses": {
            "201": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/TicketTipsListResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "tip ticket",
          "tags": [
            "support-rates"
          ]
        }
      },
      "/support/gpt": {
        "post": {
          "operationId": "SupportGptController_sendMessage",
          "parameters": [],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/SendGptMessageRequestDto"
                }
              }
            }
          },
          "responses": {
            "201": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/TicketMessageListResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "send message to gpt",
          "tags": [
            "support-gpt"
          ]
        }
      },
      "/services": {
        "get": {
          "operationId": "getList",
          "parameters": [
            {
              "name": "offset",
              "required": false,
              "in": "query",
              "schema": {
                "default": 0,
                "type": "number"
              }
            },
            {
              "name": "limit",
              "required": false,
              "in": "query",
              "schema": {
                "default": 50,
                "type": "number"
              }
            },
            {
              "name": "sort",
              "required": false,
              "in": "query",
              "schema": {
                "type": "string"
              }
            },
            {
              "name": "filter",
              "required": false,
              "in": "query",
              "schema": {
                "type": "string"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/ServicesListResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            },
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "get services list",
          "tags": [
            "services"
          ]
        }
      },
      "/services/{id}": {
        "get": {
          "operationId": "getById",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/ServiceResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            },
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "get service by id",
          "tags": [
            "services"
          ]
        },
        "patch": {
          "operationId": "ServicesController_edit",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "204": {
              "description": "service data saved"
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "edit your service data",
          "tags": [
            "services"
          ]
        },
        "delete": {
          "operationId": "requestDeletion",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "204": {
              "description": "Deletion task enqueued"
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "delete service",
          "tags": [
            "services"
          ]
        }
      },
      "/services/{id}/prices": {
        "get": {
          "operationId": "getPrices",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/ServiceTermPricesResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            },
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "get computed total price for each payment term from product rawPrices",
          "tags": [
            "services"
          ]
        }
      },
      "/services/{id}/computed-parameters": {
        "get": {
          "operationId": "getComputedParameters",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "200": {
              "description": ""
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "get service computed parameters",
          "tags": [
            "services"
          ]
        }
      },
      "/services/{id}/command/{command}": {
        "post": {
          "operationId": "ServicesController_sendCommand",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            },
            {
              "name": "command",
              "required": true,
              "in": "path",
              "schema": {
                "type": "string"
              }
            }
          ],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "type": "string"
                }
              }
            }
          },
          "responses": {
            "201": {
              "description": ""
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "send command to service pm with service",
          "tags": [
            "services"
          ]
        }
      },
      "/services/command/{command}": {
        "post": {
          "operationId": "ServicesController_sendServicelessCommand",
          "parameters": [
            {
              "name": "command",
              "required": true,
              "in": "path",
              "schema": {
                "type": "string"
              }
            },
            {
              "name": "pm",
              "required": true,
              "in": "query",
              "schema": {
                "type": "string"
              }
            }
          ],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "type": "string"
                }
              }
            }
          },
          "responses": {
            "201": {
              "description": ""
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "send command to service pm",
          "tags": [
            "services"
          ]
        }
      },
      "/services/{id}/migrate": {
        "post": {
          "operationId": "migrate",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/ServiceResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "migrate service to new product from legacy product",
          "tags": [
            "services"
          ]
        }
      },
      "/services/{id}/prolong": {
        "post": {
          "operationId": "prolong",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/ProlongServiceRequestDto"
                }
              }
            }
          },
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/BillingBuyResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "prolong service",
          "tags": [
            "services"
          ]
        }
      },
      "/services/{id}/price": {
        "get": {
          "operationId": "getPrice",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/PriceResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "get service price breakdown",
          "tags": [
            "services"
          ]
        }
      },
      "/services/{id}/change-product": {
        "get": {
          "operationId": "getChangeProductPrice",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            },
            {
              "name": "productId",
              "required": true,
              "in": "query",
              "description": "Target product id",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/ChangeProductPriceResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "get prorated price for switching service to another product in the same group",
          "tags": [
            "services"
          ]
        },
        "post": {
          "operationId": "changeProduct",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/ChangeProductRequestDto"
                }
              }
            }
          },
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/BillingBuyResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "pay and switch service to another product in the same group",
          "tags": [
            "services"
          ]
        }
      },
      "/services/{id}/stats/{statType}": {
        "get": {
          "operationId": "getStats",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            },
            {
              "name": "statType",
              "required": true,
              "in": "path",
              "schema": {
                "type": "string",
                "enum": [
                  "cpu_usage",
                  "memory_usage",
                  "net_read_mbits",
                  "net_write_mbits"
                ]
              }
            },
            {
              "name": "resolution",
              "required": true,
              "in": "query",
              "schema": {
                "type": "number"
              }
            },
            {
              "name": "fromDate",
              "required": true,
              "in": "query",
              "schema": {
                "format": "date-time",
                "type": "string"
              }
            },
            {
              "name": "toDate",
              "required": true,
              "in": "query",
              "schema": {
                "format": "date-time",
                "type": "string"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/ServiceStatsResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "get service usage stats",
          "tags": [
            "services"
          ]
        }
      },
      "/services/types": {
        "get": {
          "operationId": "getList",
          "parameters": [
            {
              "name": "offset",
              "required": false,
              "in": "query",
              "schema": {
                "default": 0,
                "type": "number"
              }
            },
            {
              "name": "limit",
              "required": false,
              "in": "query",
              "schema": {
                "default": 50,
                "type": "number"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/ProductTypesResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "get service product types list",
          "tags": [
            "service-types"
          ]
        }
      },
      "/billing/transactions": {
        "get": {
          "operationId": "getList",
          "parameters": [
            {
              "name": "offset",
              "required": false,
              "in": "query",
              "schema": {
                "default": 0,
                "type": "number"
              }
            },
            {
              "name": "limit",
              "required": false,
              "in": "query",
              "schema": {
                "default": 50,
                "type": "number"
              }
            },
            {
              "name": "sort",
              "required": false,
              "in": "query",
              "schema": {
                "type": "string"
              }
            },
            {
              "name": "filter",
              "required": false,
              "in": "query",
              "schema": {
                "type": "string"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/BillingTransactionsListResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "get transaction list",
          "tags": [
            "billing-transactions"
          ]
        }
      },
      "/billing/transactions/{id}": {
        "get": {
          "operationId": "getOne",
          "parameters": [],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/BillingTransactionResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "get transaction by id",
          "tags": [
            "billing-transactions"
          ]
        }
      },
      "/billing/invoices": {
        "post": {
          "operationId": "create",
          "parameters": [],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/CreateInvoiceRequestDto"
                }
              }
            }
          },
          "responses": {
            "201": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/InvoiceResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "",
          "tags": [
            "billing-invoices"
          ]
        }
      },
      "/billing/invoices/{id}": {
        "get": {
          "operationId": "getInvoice",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/InvoiceResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "",
          "tags": [
            "billing-invoices"
          ]
        }
      },
      "/billing/recurrent-payments/configure": {
        "post": {
          "operationId": "RecurrentPaymentsController_createRecurrentPaymentConfig",
          "parameters": [],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/ConfigureRecurrentPaymentsRequestDto"
                }
              }
            }
          },
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/RecurrentPaymentConfigResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "",
          "tags": [
            "recurrent-payments"
          ]
        }
      },
      "/billing/recurrent-payments/config": {
        "get": {
          "operationId": "RecurrentPaymentsController_getCurrentRecurrentPaymentConfig",
          "parameters": [],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/RecurrentPaymentConfigResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "",
          "tags": [
            "recurrent-payments"
          ]
        },
        "patch": {
          "operationId": "RecurrentPaymentsController_updateRecurrentPaymentConfig",
          "parameters": [],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/UpdateRecurrentPaymentsConfigRequestDto"
                }
              }
            }
          },
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/RecurrentPaymentConfigResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "",
          "tags": [
            "recurrent-payments"
          ]
        }
      },
      "/billing/recurrent-payments": {
        "delete": {
          "operationId": "RecurrentPaymentsController_deleteRecurrentPayments",
          "parameters": [],
          "responses": {
            "200": {
              "description": ""
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "",
          "tags": [
            "recurrent-payments"
          ]
        }
      },
      "/services/orders/price": {
        "get": {
          "operationId": "getPrice",
          "parameters": [
            {
              "name": "productId",
              "required": true,
              "in": "query",
              "schema": {
                "type": "number"
              }
            },
            {
              "name": "paymentTerm",
              "required": false,
              "in": "query",
              "schema": {
                "default": "month",
                "type": "string",
                "enum": [
                  "hour",
                  "half_day",
                  "day",
                  "week",
                  "month",
                  "quarter_year",
                  "half_year",
                  "year",
                  "eternal"
                ]
              }
            },
            {
              "name": "isBackupsEnabled",
              "required": false,
              "in": "query",
              "schema": {
                "default": false,
                "type": "boolean"
              }
            },
            {
              "name": "unstable_isExtendedDdosProtection",
              "required": false,
              "in": "query",
              "schema": {
                "default": false,
                "type": "boolean"
              }
            },
            {
              "name": "additionalResources",
              "required": false,
              "in": "query",
              "description": "Additional resources beyond product base values (JSON object or query object)",
              "schema": {
                "example": {
                  "cpu": 2,
                  "ram": 8192
                },
                "allOf": [
                  {
                    "$ref": "#/components/schemas/Object"
                  }
                ]
              }
            },
            {
              "name": "termCount",
              "required": false,
              "in": "query",
              "description": "Multiplier for payment term (e.g. termCount: 24 with term: hour = 24 hours)",
              "schema": {
                "minimum": 1,
                "default": 1,
                "type": "number"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/PriceResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "get price for product with parameters",
          "tags": [
            "services-orders"
          ]
        }
      },
      "/services/orders": {
        "post": {
          "operationId": "createOrder",
          "parameters": [],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/CreateOrderRequestDto"
                }
              }
            }
          },
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/BillingBuyResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "create service order",
          "tags": [
            "services-orders"
          ]
        }
      },
      "/services/limits": {
        "get": {
          "operationId": "getList",
          "parameters": [],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/ServicesLimitsListResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "",
          "tags": [
            "services-limits"
          ]
        }
      },
      "/services/ssh-keys": {
        "get": {
          "operationId": "getList",
          "parameters": [
            {
              "name": "offset",
              "required": false,
              "in": "query",
              "schema": {
                "default": 0,
                "type": "number"
              }
            },
            {
              "name": "limit",
              "required": false,
              "in": "query",
              "schema": {
                "default": 50,
                "type": "number"
              }
            },
            {
              "name": "sort",
              "required": false,
              "in": "query",
              "schema": {
                "type": "string"
              }
            },
            {
              "name": "filter",
              "required": false,
              "in": "query",
              "schema": {
                "type": "string"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/SshKeyListResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "Get SSH keys list",
          "tags": [
            "services-ssh-keys"
          ]
        },
        "post": {
          "operationId": "create",
          "parameters": [],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/CreateSshKeyRequestDto"
                }
              }
            }
          },
          "responses": {
            "201": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/SshKeyResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "Create SSH key",
          "tags": [
            "services-ssh-keys"
          ]
        }
      },
      "/services/ssh-keys/{sshKeyId}": {
        "get": {
          "operationId": "get",
          "parameters": [
            {
              "name": "sshKeyId",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/SshKeyResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "Get SSH key by id",
          "tags": [
            "services-ssh-keys"
          ]
        },
        "patch": {
          "operationId": "edit",
          "parameters": [
            {
              "name": "sshKeyId",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/EditSshKeyRequestDto"
                }
              }
            }
          },
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/SshKeyResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "Edit SSH key",
          "tags": [
            "services-ssh-keys"
          ]
        },
        "delete": {
          "operationId": "delete",
          "parameters": [
            {
              "name": "sshKeyId",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "204": {
              "description": "SSH key deleted"
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "Delete SSH key",
          "tags": [
            "services-ssh-keys"
          ]
        }
      },
      "/services/{serviceId}/networks/ipv4": {
        "get": {
          "operationId": "ServicesNetworksController_ipv4GetList",
          "parameters": [
            {
              "name": "serviceId",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/Ipv4ListResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "tags": [
            "services-networks"
          ]
        },
        "post": {
          "operationId": "buyIpv4",
          "parameters": [
            {
              "name": "serviceId",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/BuyIpv4RequestDto"
                }
              }
            }
          },
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/BillingBuyResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "Buy IPv4 address for service",
          "tags": [
            "services-networks"
          ]
        },
        "delete": {
          "operationId": "deleteIpv4",
          "parameters": [
            {
              "name": "serviceId",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/DeleteIpv4RequestDto"
                }
              }
            }
          },
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/BillingBuyResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "Delete IPv4 address",
          "tags": [
            "services-networks"
          ]
        }
      },
      "/services/{serviceId}/networks/ipv4/price": {
        "get": {
          "operationId": "ServicesNetworksController_ipv4GetPrice",
          "parameters": [
            {
              "name": "serviceId",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            },
            {
              "name": "extendedDdosProtection",
              "required": false,
              "in": "query",
              "schema": {
                "type": "boolean"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/Ipv4PriceResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "tags": [
            "services-networks"
          ]
        }
      },
      "/services/{serviceId}/networks/ipv6": {
        "get": {
          "operationId": "ServicesNetworksController_ipv6GetList",
          "parameters": [
            {
              "name": "serviceId",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/Ipv6ListResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "tags": [
            "services-networks"
          ]
        }
      },
      "/services/{serviceId}/networks/ipv4/{externalId}/edit-ptr": {
        "post": {
          "operationId": "editPtr",
          "parameters": [
            {
              "name": "serviceId",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            },
            {
              "name": "externalId",
              "required": true,
              "in": "path",
              "schema": {
                "type": "string"
              }
            }
          ],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/ServicesNetworksEditPtrRequestDto"
                }
              }
            }
          },
          "responses": {
            "204": {
              "description": ""
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "Edit PTR record",
          "tags": [
            "services-networks"
          ]
        }
      },
      "/services/{serviceId}/networks/ipv4/{externalId}/make-main": {
        "post": {
          "operationId": "makeMain",
          "parameters": [
            {
              "name": "serviceId",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            },
            {
              "name": "externalId",
              "required": true,
              "in": "path",
              "schema": {
                "type": "string"
              }
            }
          ],
          "responses": {
            "204": {
              "description": ""
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "Make netork as main",
          "tags": [
            "services-networks"
          ]
        }
      },
      "/services/{id}/resources": {
        "get": {
          "operationId": "getResources",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/ServiceResourcesResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "get service resources information",
          "tags": [
            "services-resources"
          ]
        }
      },
      "/services/{id}/additional-resources/get-price": {
        "post": {
          "operationId": "getAdditionalResourcesPrice",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/GetAdditionalResourcesPriceRequestDto"
                }
              }
            }
          },
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/GetAdditionalResourcesPriceResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "get additional resources price",
          "tags": [
            "services-resources"
          ]
        }
      },
      "/services/{id}/additional-resources": {
        "post": {
          "operationId": "addAdditionalResources",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/AddAdditionalResourcesRequestDto"
                }
              }
            }
          },
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/BillingBuyResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "add additional resources to service",
          "tags": [
            "services-resources"
          ]
        }
      },
      "/services/static-links": {
        "get": {
          "operationId": "callStaticLink",
          "parameters": [
            {
              "name": "key",
              "required": true,
              "in": "query",
              "schema": {
                "type": "string"
              }
            }
          ],
          "responses": {
            "200": {
              "description": ""
            }
          },
          "summary": "Get service static info by key",
          "tags": [
            "services-static-links"
          ]
        }
      },
      "/services/{id}/ctl/resume": {
        "post": {
          "operationId": "resume",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "204": {
              "description": ""
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "Resume service",
          "tags": [
            "services"
          ]
        }
      },
      "/services/{id}/ctl/suspend": {
        "post": {
          "operationId": "suspend",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/ServiceCtlForceRequestDto"
                }
              }
            }
          },
          "responses": {
            "204": {
              "description": ""
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "Suspend service",
          "tags": [
            "services"
          ]
        }
      },
      "/services/{id}/ctl/restart": {
        "post": {
          "operationId": "restart",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/ServiceCtlForceRequestDto"
                }
              }
            }
          },
          "responses": {
            "204": {
              "description": ""
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "Restart service",
          "tags": [
            "services"
          ]
        }
      },
      "/services/{id}/rescue": {
        "post": {
          "operationId": "enterRescueMode",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "204": {
              "description": ""
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "enter to rescue mode",
          "tags": [
            "services"
          ]
        }
      },
      "/services/{id}/rescue/leave": {
        "post": {
          "operationId": "leaveRescueMode",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "204": {
              "description": "rescue mode left"
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "leave rescue mode",
          "tags": [
            "services"
          ]
        }
      },
      "/services/{id}/remote/vnc": {
        "post": {
          "operationId": "connectRemoteVnc",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/RemoteVncResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "create vnc connection",
          "tags": [
            "services"
          ]
        }
      },
      "/services/{id}/change-password": {
        "post": {
          "operationId": "changePassword",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/ChangePasswordRequestDto"
                }
              }
            }
          },
          "responses": {
            "204": {
              "description": ""
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "change service password",
          "tags": [
            "services"
          ]
        }
      },
      "/services/{id}/reinstall": {
        "post": {
          "operationId": "reinstall",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/ReinstallServiceRequestDto"
                }
              }
            }
          },
          "responses": {
            "204": {
              "description": ""
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "reinstall service",
          "tags": [
            "services"
          ]
        }
      },
      "/services/{id}/goto": {
        "post": {
          "operationId": "goto",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "type": "object",
                    "properties": {
                      "link": {
                        "type": "string"
                      }
                    }
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "get goto link for service",
          "tags": [
            "services"
          ]
        }
      },
      "/services/operating-systems": {
        "get": {
          "operationId": "getList",
          "parameters": [
            {
              "name": "offset",
              "required": false,
              "in": "query",
              "schema": {
                "default": 0,
                "type": "number"
              }
            },
            {
              "name": "limit",
              "required": false,
              "in": "query",
              "schema": {
                "default": 50,
                "type": "number"
              }
            },
            {
              "name": "sort",
              "required": false,
              "in": "query",
              "schema": {
                "type": "string"
              }
            },
            {
              "name": "filter",
              "required": false,
              "in": "query",
              "schema": {
                "type": "string"
              }
            }
          ],
          "responses": {
            "200": {
              "description": ""
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "",
          "tags": [
            "services-operating-systems"
          ]
        }
      },
      "/services/operating-systems/recipes": {
        "get": {
          "operationId": "getRecipesList",
          "parameters": [],
          "responses": {
            "200": {
              "description": ""
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "",
          "tags": [
            "services-operating-systems"
          ]
        }
      },
      "/services/{serviceId}/backups": {
        "get": {
          "operationId": "getList",
          "parameters": [
            {
              "name": "serviceId",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            },
            {
              "name": "offset",
              "required": false,
              "in": "query",
              "schema": {
                "default": 0,
                "type": "number"
              }
            },
            {
              "name": "limit",
              "required": false,
              "in": "query",
              "schema": {
                "default": 50,
                "type": "number"
              }
            },
            {
              "name": "sort",
              "required": false,
              "in": "query",
              "schema": {
                "type": "string"
              }
            },
            {
              "name": "filter",
              "required": false,
              "in": "query",
              "schema": {
                "type": "string"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/ServiceBackupListResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "get backups",
          "tags": [
            "service-backups"
          ]
        },
        "post": {
          "operationId": "create",
          "parameters": [
            {
              "name": "serviceId",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/CreateServiceBackupRequestDto"
                }
              }
            }
          },
          "responses": {
            "201": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/ServiceBackupResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "create backup",
          "tags": [
            "service-backups"
          ]
        }
      },
      "/services/{serviceId}/backups/{backupId}": {
        "delete": {
          "operationId": "delete",
          "parameters": [
            {
              "name": "serviceId",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            },
            {
              "name": "backupId",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "204": {
              "description": ""
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "delete backup",
          "tags": [
            "service-backups"
          ]
        }
      },
      "/services/{serviceId}/backups/{backupId}/restore": {
        "post": {
          "operationId": "restore",
          "parameters": [
            {
              "name": "serviceId",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            },
            {
              "name": "backupId",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "204": {
              "description": ""
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "restore backup",
          "tags": [
            "service-backups"
          ]
        }
      },
      "/services/{serviceId}/backups/schedule": {
        "post": {
          "operationId": "setSchedule",
          "parameters": [
            {
              "name": "serviceId",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/SetServiceBackupScheduleRequestDto"
                }
              }
            }
          },
          "responses": {
            "204": {
              "description": ""
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "set schedule for service",
          "tags": [
            "service-backups"
          ]
        },
        "delete": {
          "operationId": "deleteSchedule",
          "parameters": [
            {
              "name": "serviceId",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "204": {
              "description": ""
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "delete schedule for service",
          "tags": [
            "service-backups"
          ]
        }
      },
      "/services/{serviceId}/backups/price": {
        "get": {
          "operationId": "getBackupsPrice",
          "parameters": [
            {
              "name": "serviceId",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "200": {
              "description": ""
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "get backups price",
          "tags": [
            "service-backups"
          ]
        }
      },
      "/services/{serviceId}/backups/enable": {
        "post": {
          "operationId": "enableBackups",
          "parameters": [
            {
              "name": "serviceId",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/BillingBuyRequestDto"
                }
              }
            }
          },
          "responses": {
            "201": {
              "description": ""
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "enable backups",
          "tags": [
            "service-backups"
          ]
        }
      },
      "/services/{serviceId}/backups/disable": {
        "post": {
          "operationId": "disableBackups",
          "parameters": [
            {
              "name": "serviceId",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "201": {
              "description": ""
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "disable backups",
          "tags": [
            "service-backups"
          ]
        }
      },
      "/domains": {
        "get": {
          "operationId": "getList",
          "parameters": [
            {
              "name": "offset",
              "required": false,
              "in": "query",
              "schema": {
                "default": 0,
                "type": "number"
              }
            },
            {
              "name": "limit",
              "required": false,
              "in": "query",
              "schema": {
                "default": 50,
                "type": "number"
              }
            },
            {
              "name": "sort",
              "required": false,
              "in": "query",
              "schema": {
                "type": "string"
              }
            },
            {
              "name": "filter",
              "required": false,
              "in": "query",
              "schema": {
                "type": "string"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/DomainListResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "",
          "tags": [
            "domains"
          ]
        },
        "post": {
          "operationId": "create",
          "parameters": [],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/CreateDomainRequestDto"
                }
              }
            }
          },
          "responses": {
            "201": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/DomainResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "",
          "tags": [
            "domains"
          ]
        }
      },
      "/domains/{id}": {
        "get": {
          "operationId": "get",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/DomainResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "",
          "tags": [
            "domains"
          ]
        },
        "delete": {
          "operationId": "delete",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/DeleteDomainResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "",
          "tags": [
            "domains"
          ]
        }
      },
      "/domains/{id}/schedule-ns-check": {
        "post": {
          "operationId": "scheduleNsCheck",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "204": {
              "description": ""
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "",
          "tags": [
            "domains"
          ]
        }
      },
      "/domains/check-delegation": {
        "post": {
          "description": "Checks whether the given domain belongs to the caller, is registered in billing, and is fully delegated to our nameservers.",
          "operationId": "checkDomainDelegation",
          "parameters": [],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/CheckDomainDelegationRequestDto"
                }
              }
            }
          },
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/CheckDomainDelegationResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "",
          "tags": [
            "domains"
          ]
        }
      },
      "/domains/nameservers": {
        "get": {
          "operationId": "getExpectedNameservers",
          "parameters": [],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/DomainExpectedNameserversListResponseDto"
                  }
                }
              }
            }
          },
          "summary": "Get expected nameservers for domain delegation",
          "tags": [
            "domains"
          ]
        }
      },
      "/domains/record-types": {
        "get": {
          "operationId": "getRecordTypes",
          "parameters": [],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/DomainZoneRecordTypesListResponseDto"
                  }
                }
              }
            }
          },
          "summary": "Get supported DNS record types and validation filters",
          "tags": [
            "domains"
          ]
        }
      },
      "/domains/{domainId}/records": {
        "get": {
          "operationId": "getRecordsList",
          "parameters": [
            {
              "name": "domainId",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            },
            {
              "name": "offset",
              "required": false,
              "in": "query",
              "schema": {
                "default": 0,
                "type": "number"
              }
            },
            {
              "name": "limit",
              "required": false,
              "in": "query",
              "schema": {
                "default": 50,
                "type": "number"
              }
            },
            {
              "name": "sort",
              "required": false,
              "in": "query",
              "schema": {
                "type": "string"
              }
            },
            {
              "name": "filter",
              "required": false,
              "in": "query",
              "schema": {
                "type": "string"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/DomainRecordListResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "",
          "tags": [
            "domains"
          ]
        },
        "post": {
          "operationId": "createRecord",
          "parameters": [
            {
              "name": "domainId",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/CreateDomainRecordRequestDto"
                }
              }
            }
          },
          "responses": {
            "201": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/DomainRecordResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "",
          "tags": [
            "domains"
          ]
        }
      },
      "/domains/{domainId}/records/{recordId}": {
        "get": {
          "operationId": "getRecord",
          "parameters": [
            {
              "name": "domainId",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/DomainRecordResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "",
          "tags": [
            "domains"
          ]
        },
        "patch": {
          "operationId": "editRecord",
          "parameters": [
            {
              "name": "domainId",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/EditDomainRecordRequestDto"
                }
              }
            }
          },
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/DomainRecordResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "",
          "tags": [
            "domains"
          ]
        },
        "delete": {
          "operationId": "deleteRecord",
          "parameters": [
            {
              "name": "domainId",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "204": {
              "description": ""
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "",
          "tags": [
            "domains"
          ]
        }
      },
      "/gifts": {
        "get": {
          "operationId": "GiftsController_getOwnedGifts",
          "parameters": [],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/GiftsResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "tags": [
            "gifts"
          ]
        },
        "post": {
          "operationId": "GiftsController_buyGift",
          "parameters": [],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/BuyGiftRequestDto"
                }
              }
            }
          },
          "responses": {
            "201": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/InvoiceResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "tags": [
            "gifts"
          ]
        }
      },
      "/gifts/styles": {
        "get": {
          "operationId": "getGiftStyles",
          "parameters": [],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/AvailableGiftsStylesResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "get gift styles",
          "tags": [
            "gifts"
          ]
        }
      },
      "/gifts/{id}/regen": {
        "post": {
          "operationId": "GiftsController_regen",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "204": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "tags": [
            "gifts"
          ]
        }
      },
      "/gifts/by-code/{code}": {
        "get": {
          "operationId": "GiftsController_getByCode",
          "parameters": [
            {
              "name": "code",
              "required": true,
              "in": "path",
              "schema": {
                "type": "string"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/GiftListResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "tags": [
            "gifts"
          ]
        }
      },
      "/gifts/by-code/{code}/use": {
        "post": {
          "operationId": "GiftsController_useCode",
          "parameters": [
            {
              "name": "code",
              "required": true,
              "in": "path",
              "schema": {
                "type": "string"
              }
            }
          ],
          "responses": {
            "204": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "tags": [
            "gifts"
          ]
        }
      },
      "/gifts/{id}": {
        "put": {
          "operationId": "GiftsController_edit",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/EditGiftRequestDto"
                }
              }
            }
          },
          "responses": {
            "204": {
              "description": "the gift has been edited"
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "edit gift",
          "tags": [
            "gifts"
          ]
        }
      },
      "/bonuses": {
        "get": {
          "operationId": "getList",
          "parameters": [],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/BonusesResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "Get bonuses list",
          "tags": [
            "bonuses"
          ]
        }
      },
      "/bonuses/{id}/activate": {
        "post": {
          "operationId": "BonusesController_activate",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "201": {
              "description": ""
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "tags": [
            "bonuses"
          ]
        }
      },
      "/bonuses/bonus-case/{id}/activate": {
        "post": {
          "operationId": "BonusesController_activateBonusCase",
          "parameters": [
            {
              "name": "id",
              "required": true,
              "in": "path",
              "schema": {
                "type": "number"
              }
            }
          ],
          "responses": {
            "201": {
              "description": ""
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "tags": [
            "bonuses"
          ]
        }
      },
      "/leakcheck/aeza": {
        "get": {
          "operationId": "LeakcheckController_leackCheck",
          "parameters": [],
          "responses": {
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            },
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "tags": [
            "leakcheck"
          ]
        }
      },
      "/promo/giftcodes/{code}/use": {
        "post": {
          "operationId": "use",
          "parameters": [
            {
              "name": "code",
              "required": true,
              "in": "path",
              "schema": {
                "type": "string"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/UseGiftcodeResponseDto"
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "use giftcode",
          "tags": [
            "promo-giftcodes"
          ]
        }
      },
      "/system/alerts": {
        "get": {
          "operationId": "getList",
          "parameters": [
            {
              "name": "slots",
              "required": true,
              "in": "query",
              "schema": {
                "type": "string"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "List of active system alerts",
              "content": {
                "application/json": {
                  "schema": {
                    "type": "array",
                    "items": {
                      "$ref": "#/components/schemas/SystemAlertResponseDto"
                    }
                  }
                }
              }
            }
          },
          "summary": "Get active system alerts by slots",
          "tags": [
            "system"
          ]
        }
      },
      "/health": {
        "get": {
          "operationId": "OtherController_getHealth",
          "parameters": [],
          "responses": {
            "200": {
              "description": ""
            }
          },
          "tags": [
            "other"
          ]
        }
      },
      "/version": {
        "get": {
          "operationId": "OtherController_getVersion",
          "parameters": [],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "type": "string"
                  }
                }
              }
            }
          },
          "tags": [
            "other"
          ]
        }
      },
      "/errors": {
        "get": {
          "operationId": "OtherController_getErrors",
          "parameters": [],
          "responses": {
            "200": {
              "description": ""
            }
          },
          "tags": [
            "other"
          ]
        }
      },
      "/unsubscribe": {
        "post": {
          "operationId": "OtherController_unsubscribeMailChallenge",
          "parameters": [],
          "requestBody": {
            "required": true,
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/UnsubscribeMailRequestDto"
                }
              }
            }
          },
          "responses": {
            "204": {
              "description": "Successfully unsubscribe from the mailing list by email"
            }
          },
          "summary": "unsubscribe from unwanted mailings.",
          "tags": [
            "other"
          ]
        }
      },
      "/info": {
        "get": {
          "operationId": "OtherController_getInfo",
          "parameters": [],
          "responses": {
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "tags": [
            "other"
          ]
        }
      },
      "/files-staff/files": {
        "get": {
          "operationId": "FilesStaffController_getById",
          "parameters": [
            {
              "name": "fileIds",
              "required": false,
              "in": "query",
              "schema": {
                "type": "string"
              }
            }
          ],
          "responses": {
            "200": {
              "description": "",
              "content": {
                "application/json": {
                  "schema": {
                    "type": "array",
                    "items": {
                      "$ref": "#/components/schemas/FileResponseDto"
                    }
                  }
                }
              }
            },
            "403": {
              "description": "not_auth"
            }
          },
          "security": [
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            },
            {
              "bearer": []
            },
            {
              "X-API-KEY": []
            }
          ],
          "summary": "get files by id",
          "tags": [
            "FilesStaff"
          ]
        }
      }
    },
    "info": {
      "title": "aeza",
      "description": "",
      "version": "c935cfac",
      "contact": {}
    },
    "tags": [],
    "servers": [
      {
        "url": "/api/v2"
      }
    ],
    "components": {
      "securitySchemes": {
        "bearer": {
          "scheme": "bearer",
          "bearerFormat": "JWT",
          "type": "http"
        },
        "X-API-KEY": {
          "type": "apiKey",
          "in": "header",
          "name": "X-API-KEY"
        }
      },
      "schemas": {
        "CheckDomainAvailableRequestDto": {
          "type": "object",
          "properties": {
            "name": {
              "type": "string",
              "description": "Domain name to check availability for (e.g. \"example\" or \"example.com\")",
              "example": "example"
            },
            "count": {
              "type": "number",
              "description": "Limit the number of zones to return",
              "example": 5
            },
            "excludes": {
              "description": "Zone product IDs to exclude from check",
              "example": [
                1,
                2
              ],
              "type": "array",
              "items": {
                "type": "string"
              }
            },
            "challenge": {
              "type": "string",
              "description": "WebSocket challenge token for Centrifugo events",
              "example": "abc123"
            }
          },
          "required": [
            "name"
          ]
        },
        "ZoneCheckItemResponseDto": {
          "type": "object",
          "properties": {
            "id": {
              "type": "number",
              "description": "Zone product ID",
              "example": 1
            },
            "title": {
              "type": "string",
              "description": "Full domain name",
              "example": "example.com"
            },
            "order": {
              "type": "number",
              "description": "Display order (matched zone has -1)",
              "example": -1
            },
            "individualPrice": {
              "type": "number",
              "description": "Yearly price for this zone",
              "example": 999
            },
            "isEquals": {
              "type": "boolean",
              "description": "Whether this zone matched the input domain",
              "example": true
            },
            "available": {
              "type": "boolean",
              "description": "Whether the domain is available for registration",
              "example": true
            }
          },
          "required": [
            "id",
            "title",
            "order",
            "individualPrice",
            "isEquals"
          ]
        },
        "CheckDomainAvailableResponseDto": {
          "type": "object",
          "properties": {
            "count": {
              "type": "number",
              "description": "Total number of available zones (remaining after count limit)",
              "example": 50
            },
            "items": {
              "description": "List of zone check results",
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/ZoneCheckItemResponseDto"
              }
            }
          },
          "required": [
            "count",
            "items"
          ]
        },
        "ProductResourcePricesResponseDto": {
          "type": "object",
          "properties": {
            "base": {
              "type": "object",
              "description": "the amount for the extension or purchase of the resource"
            },
            "first": {
              "type": "object",
              "description": "custom amount for the purchase of a resource (replaces base price)"
            },
            "install": {
              "type": "number",
              "description": "additional amount for the installation of the resource"
            }
          },
          "required": [
            "base"
          ]
        },
        "ProductTypeNestedResponseDto": {
          "type": "object",
          "properties": {
            "slug": {
              "type": "string"
            },
            "name": {
              "type": "string"
            }
          },
          "required": [
            "slug",
            "name"
          ]
        },
        "ProductGroupNestedResponseDto": {
          "type": "object",
          "properties": {
            "id": {
              "type": "number"
            },
            "name": {
              "type": "string"
            }
          },
          "required": [
            "id",
            "name"
          ]
        },
        "ProductResourceResponseDto": {
          "type": "object",
          "properties": {
            "price": {
              "type": "number",
              "description": "Price per unit of resource per month (in minimum currency units)"
            },
            "max": {
              "type": "number",
              "description": "Maximum resource value (defaults to base tariff value if not specified)"
            },
            "step": {
              "type": "number",
              "description": "Resource change step (defaults to 1 if not specified)"
            }
          }
        },
        "ProductResponseDto": {
          "type": "object",
          "properties": {
            "id": {
              "type": "number"
            },
            "name": {
              "type": "string"
            },
            "typeSlug": {
              "type": "string"
            },
            "typeName": {
              "type": "string"
            },
            "groupId": {
              "type": "number"
            },
            "fullPayload": {
              "type": "object"
            },
            "payload": {
              "type": "object"
            },
            "prices": {
              "$ref": "#/components/schemas/ProductResourcePricesResponseDto"
            },
            "localedPayload": {
              "type": "object"
            },
            "tags": {
              "type": "array",
              "items": {
                "type": "string"
              }
            },
            "type": {
              "$ref": "#/components/schemas/ProductTypeNestedResponseDto"
            },
            "group": {
              "$ref": "#/components/schemas/ProductGroupNestedResponseDto"
            },
            "order": {
              "type": "number"
            },
            "parameters": {
              "type": "object",
              "description": "Base product parameters"
            },
            "isPublic": {
              "type": "boolean",
              "description": "Доступен для покупки всем пользователям"
            },
            "isAvailable": {
              "type": "boolean"
            },
            "rawPrices": {
              "type": "object"
            },
            "firstPrices": {
              "type": "object"
            },
            "resources": {
              "$ref": "#/components/schemas/ProductResourceResponseDto"
            }
          },
          "required": [
            "id",
            "name",
            "typeSlug",
            "typeName",
            "groupId",
            "fullPayload",
            "payload",
            "prices",
            "localedPayload",
            "tags",
            "type",
            "group",
            "order",
            "parameters",
            "isPublic",
            "isAvailable",
            "rawPrices",
            "firstPrices",
            "resources"
          ]
        },
        "ProductsResponseDto": {
          "type": "object",
          "properties": {
            "items": {
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/ProductResponseDto"
              }
            },
            "total": {
              "type": "number"
            }
          },
          "required": [
            "items",
            "total"
          ]
        },
        "ProductTypeParameterResponse": {
          "type": "object",
          "properties": {
            "field": {
              "type": "string"
            },
            "name": {
              "type": "string"
            },
            "type": {
              "type": "string"
            }
          },
          "required": [
            "field",
            "name",
            "type"
          ]
        },
        "ProductTypeResponseDto": {
          "type": "object",
          "properties": {
            "slug": {
              "type": "string"
            },
            "name": {
              "type": "string"
            },
            "parameters": {
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/ProductTypeParameterResponse"
              }
            },
            "serviceHandler": {
              "type": "string"
            },
            "payload": {
              "type": "object"
            },
            "localedPayload": {
              "type": "object"
            }
          },
          "required": [
            "slug",
            "name",
            "parameters",
            "serviceHandler",
            "payload",
            "localedPayload"
          ]
        },
        "ProductGroupResponseDto": {
          "type": "object",
          "properties": {
            "id": {
              "type": "number"
            },
            "name": {
              "type": "string"
            },
            "type": {
              "$ref": "#/components/schemas/ProductTypeResponseDto"
            },
            "role": {
              "type": "string"
            },
            "parentId": {
              "type": "number"
            },
            "description": {
              "type": "string"
            },
            "payload": {
              "type": "object"
            },
            "isAvailable": {
              "type": "boolean"
            },
            "localedPayload": {
              "type": "object"
            }
          },
          "required": [
            "id",
            "name",
            "type",
            "role",
            "parentId",
            "description",
            "payload",
            "isAvailable",
            "localedPayload"
          ]
        },
        "ProductGroupsResponseDto": {
          "type": "object",
          "properties": {
            "items": {
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/ProductGroupResponseDto"
              }
            },
            "total": {
              "type": "number"
            }
          },
          "required": [
            "items",
            "total"
          ]
        },
        "CentrifugoAuthTokenResponseDto": {
          "type": "object",
          "properties": {
            "token": {
              "type": "string"
            }
          },
          "required": [
            "token"
          ]
        },
        "ChangePasswordRequestDto": {
          "type": "object",
          "properties": {
            "password": {
              "type": "string",
              "description": "New password for the service",
              "example": "MySecurePassword123",
              "minLength": 8,
              "maxLength": 32
            }
          },
          "required": [
            "password"
          ]
        },
        "AccountBonusState": {
          "type": "string",
          "enum": [
            "not_used",
            "locked",
            "unlocked"
          ]
        },
        "AccountInterfaceResponseDto": {
          "type": "object",
          "properties": {
            "lang": {
              "type": "string",
              "example": "ru"
            },
            "currency": {
              "type": "string",
              "example": "eur"
            },
            "theme": {
              "type": "string",
              "example": "light"
            }
          },
          "required": [
            "lang",
            "currency",
            "theme"
          ]
        },
        "AccountLegalResponseDto": {
          "type": "object",
          "properties": {
            "name": {
              "type": "string"
            },
            "ogrn": {
              "type": "string",
              "example": "1217800095248"
            },
            "kpp": {
              "type": "string",
              "example": "781001001"
            },
            "inn": {
              "type": "string",
              "example": "7813654490"
            },
            "address": {
              "type": "string",
              "example": "196084, город Санкт-Петербург, Московский пр-кт, д. 97 литера А, помещ. 27-н"
            },
            "bik": {
              "type": "string"
            },
            "account": {
              "type": "string"
            },
            "corrAccount": {
              "type": "string"
            },
            "comment": {
              "type": "string"
            }
          },
          "required": [
            "name",
            "ogrn",
            "kpp",
            "inn",
            "address",
            "bik",
            "account",
            "corrAccount",
            "comment"
          ]
        },
        "AccountProfileResponseDto": {
          "type": "object",
          "properties": {
            "name": {
              "type": "string"
            },
            "names": {
              "type": "array",
              "items": {
                "type": "string"
              }
            },
            "phone": {
              "type": "string"
            },
            "type": {
              "type": "string"
            },
            "phoneConfirmed": {
              "type": "boolean"
            }
          },
          "required": [
            "phoneConfirmed"
          ]
        },
        "AccountRegion": {
          "type": "string",
          "enum": [
            "global",
            "ru"
          ]
        },
        "AccountResponseDto": {
          "type": "object",
          "properties": {
            "id": {
              "type": "number"
            },
            "email": {
              "type": "string"
            },
            "photo": {
              "type": "string"
            },
            "balance": {
              "type": "number"
            },
            "withdrawBalance": {
              "type": "number"
            },
            "totalReplenished": {
              "type": "number"
            },
            "bonusBalance": {
              "type": "number"
            },
            "referrerProgramId": {
              "type": "number",
              "nullable": true
            },
            "bonusState": {
              "allOf": [
                {
                  "$ref": "#/components/schemas/AccountBonusState"
                }
              ]
            },
            "tfa": {
              "type": "string",
              "nullable": true
            },
            "tfaEnabled": {
              "type": "boolean"
            },
            "interface": {
              "$ref": "#/components/schemas/AccountInterfaceResponseDto"
            },
            "legal": {
              "nullable": true,
              "type": "object",
              "allOf": [
                {
                  "$ref": "#/components/schemas/AccountLegalResponseDto"
                }
              ]
            },
            "payload": {
              "type": "object"
            },
            "connectedServices": {
              "type": "object"
            },
            "internalPayload": {
              "type": "object"
            },
            "discount": {
              "type": "object"
            },
            "permittedDebt": {
              "type": "number"
            },
            "profile": {
              "$ref": "#/components/schemas/AccountProfileResponseDto"
            },
            "roles": {
              "type": "array",
              "items": {
                "type": "string"
              }
            },
            "region": {
              "allOf": [
                {
                  "$ref": "#/components/schemas/AccountRegion"
                }
              ]
            },
            "currency": {
              "type": "string"
            },
            "withdrawMetadata": {
              "type": "object",
              "description": "Withdrawal metadata (auto-withdraw settings, etc.)",
              "nullable": true
            }
          },
          "required": [
            "id",
            "email",
            "photo",
            "balance",
            "withdrawBalance",
            "totalReplenished",
            "bonusBalance",
            "referrerProgramId",
            "bonusState",
            "tfaEnabled",
            "interface",
            "legal",
            "payload",
            "connectedServices",
            "internalPayload",
            "discount",
            "permittedDebt",
            "profile",
            "roles",
            "region",
            "currency",
            "withdrawMetadata"
          ]
        },
        "AccountInterfacePatchRequestDto": {
          "type": "object",
          "properties": {
            "lang": {
              "type": "string",
              "description": "Interface language code",
              "enum": [
                "ru",
                "en",
                "uk"
              ]
            },
            "theme": {
              "type": "string",
              "enum": [
                "light",
                "dark"
              ]
            }
          }
        },
        "AccountProfileType": {
          "type": "string",
          "enum": [
            "legal",
            "person"
          ]
        },
        "AccountProfilePatchRequestDto": {
          "type": "object",
          "properties": {
            "name": {
              "type": "string",
              "maxLength": 32
            },
            "type": {
              "allOf": [
                {
                  "$ref": "#/components/schemas/AccountProfileType"
                }
              ]
            }
          }
        },
        "AccountLegalPatchRequestDto": {
          "type": "object",
          "properties": {
            "name": {
              "type": "string"
            },
            "ogrn": {
              "type": "string"
            },
            "kpp": {
              "type": "string"
            },
            "inn": {
              "type": "string"
            },
            "address": {
              "type": "string"
            },
            "bik": {
              "type": "string"
            },
            "account": {
              "type": "string"
            },
            "corrAccount": {
              "type": "string"
            },
            "comment": {
              "type": "string"
            }
          }
        },
        "PatchWithdrawMetadataRequestDto": {
          "type": "object",
          "properties": {
            "autoWithdrawCryptoWallet": {
              "type": "string",
              "description": "Wallet address for automatic crypto withdrawal",
              "example": "TXYZ..."
            },
            "autoWithdrawCryptoMethod": {
              "type": "string",
              "description": "Crypto method slug for auto-withdrawal (btc, usdt, usdt_trc20, eth, xmr, ton)",
              "example": "usdt_tron"
            },
            "autoWithdrawThreshold": {
              "type": "number",
              "description": "Balance threshold for auto-withdrawal in currency units",
              "example": 5000
            }
          }
        },
        "PatchAccountMeRequestDto": {
          "type": "object",
          "properties": {
            "interface": {
              "$ref": "#/components/schemas/AccountInterfacePatchRequestDto"
            },
            "profile": {
              "$ref": "#/components/schemas/AccountProfilePatchRequestDto"
            },
            "legal": {
              "description": "Legal entity details (only for accounts on the RU billing region)",
              "allOf": [
                {
                  "$ref": "#/components/schemas/AccountLegalPatchRequestDto"
                }
              ]
            },
            "newsletterSubscribed": {
              "type": "boolean",
              "description": "Newsletter subscription flag"
            },
            "withdrawMetadata": {
              "nullable": true,
              "description": "Withdrawal metadata settings (auto-withdraw wallet, method, threshold). Pass null to clear all settings.",
              "type": "object",
              "allOf": [
                {
                  "$ref": "#/components/schemas/PatchWithdrawMetadataRequestDto"
                }
              ]
            }
          }
        },
        "ChangePhoneRequestDto": {
          "type": "object",
          "properties": {
            "phone": {
              "type": "string"
            },
            "challenge": {
              "type": "string"
            }
          },
          "required": [
            "phone"
          ]
        },
        "ConfirmPhoneRequestDto": {
          "type": "object",
          "properties": {
            "code": {
              "type": "string",
              "minLength": 4,
              "maxLength": 4
            }
          },
          "required": [
            "code"
          ]
        },
        "ReferralProgramsItemResponseDto": {
          "type": "object",
          "properties": {
            "id": {
              "type": "number",
              "example": 1,
              "description": "Unique identifier of the referral program"
            },
            "code": {
              "type": "string",
              "example": "user12345",
              "description": "Code of the referral program"
            },
            "name": {
              "type": "string",
              "example": "User's unique Referral program",
              "description": "Name of the referral program"
            },
            "ownerId": {
              "type": "number",
              "example": 1,
              "description": "Owner of the referral program"
            },
            "percent": {
              "type": "number",
              "example": 0.15,
              "description": "Reward percent of the referral program"
            },
            "customPercent": {
              "type": "number",
              "example": 1000,
              "description": "Custom reward percent of the referral program",
              "nullable": true
            },
            "isDefault": {
              "type": "boolean",
              "example": true,
              "description": "Is this referral program default for user"
            },
            "total": {
              "type": "object",
              "example": {},
              "description": "Referral program stats"
            }
          },
          "required": [
            "id",
            "code",
            "name",
            "ownerId",
            "percent",
            "customPercent",
            "isDefault",
            "total"
          ]
        },
        "ReferralProgramsTopItemResponseDto": {
          "type": "object",
          "properties": {
            "id": {
              "type": "number",
              "example": 1,
              "description": "Identifier of the referral program"
            },
            "rank": {
              "type": "number",
              "example": 1,
              "description": "Current position in the top"
            },
            "emailMask": {
              "type": "string",
              "example": "to**********",
              "description": "Masked email address of user"
            },
            "totalEarned": {
              "type": "number",
              "example": 1,
              "description": "Total earned of the referral program"
            },
            "monthlyEarned": {
              "type": "number",
              "example": 1,
              "description": "Monthly earned of the referral program"
            },
            "shiftPosition": {
              "type": "number",
              "example": 1,
              "description": "Shift position of the referral program"
            }
          },
          "required": [
            "id",
            "rank",
            "emailMask",
            "totalEarned",
            "monthlyEarned",
            "shiftPosition"
          ]
        },
        "ReferralProgramsTopResponseDto": {
          "type": "object",
          "properties": {
            "items": {
              "description": "Top items of referral programs",
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/ReferralProgramsTopItemResponseDto"
              }
            },
            "date": {
              "format": "date-time",
              "type": "string",
              "description": "Date of the top"
            }
          },
          "required": [
            "items",
            "date"
          ]
        },
        "ReferralProgramsHistoryItemResponseDto": {
          "type": "object",
          "properties": {
            "id": {
              "type": "number",
              "example": 1,
              "description": "Unique identifier of the referral history record"
            },
            "action": {
              "type": "string",
              "example": "invoice",
              "description": "Type of action that triggered the referral reward",
              "enum": [
                "registration",
                "invoice",
                "payment"
              ]
            },
            "targetId": {
              "type": "string",
              "example": "service:2",
              "description": "Target id"
            },
            "earned": {
              "type": "number",
              "example": 1000,
              "description": "Amount earned from this referral action"
            },
            "createdAt": {
              "format": "date-time",
              "type": "string",
              "example": "2021-01-01T00:00:00.000Z",
              "description": "Timestamp when the referral action was recorded"
            },
            "email": {
              "type": "string",
              "example": "te*************com",
              "description": "Masked email address of the referred account"
            }
          },
          "required": [
            "id",
            "action",
            "targetId",
            "earned",
            "createdAt",
            "email"
          ]
        },
        "ReferralProgramsHistoryResponseDto": {
          "type": "object",
          "properties": {
            "items": {
              "description": "History items of referral programs",
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/ReferralProgramsHistoryItemResponseDto"
              }
            },
            "total": {
              "type": "number",
              "description": "Count of the history items"
            }
          },
          "required": [
            "items",
            "total"
          ]
        },
        "ReferralTierResponseDto": {
          "type": "object",
          "properties": {
            "volume": {
              "type": "number",
              "example": 1000,
              "description": "Volume of earnings at which the percent is reached"
            },
            "percent": {
              "type": "number",
              "example": 0.15,
              "description": "Percent of earnings reached at the volume"
            }
          },
          "required": [
            "volume",
            "percent"
          ]
        },
        "ReferralTiersResponseDto": {
          "type": "object",
          "properties": {
            "tiers": {
              "description": "List of referral tiers",
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/ReferralTierResponseDto"
              }
            }
          },
          "required": [
            "tiers"
          ]
        },
        "InitWithdrawRequestDto": {
          "type": "object",
          "properties": {
            "amount": {
              "type": "number"
            },
            "methodId": {
              "type": "number",
              "description": "GET /withdraws/methods, the withdrawal will be made to the balance if it is not specified"
            },
            "wallet": {
              "type": "string"
            },
            "fileIds": {
              "type": "array",
              "items": {
                "type": "string"
              }
            }
          },
          "required": [
            "amount"
          ]
        },
        "WithdrawListResponseDto": {
          "type": "object",
          "properties": {
            "id": {
              "type": "number"
            },
            "amount": {
              "type": "number"
            },
            "status": {
              "type": "string",
              "enum": [
                "pending",
                "cancelled",
                "success",
                "docs_review"
              ]
            },
            "externalId": {
              "type": "string"
            },
            "createdAt": {
              "format": "date-time",
              "type": "string"
            },
            "wallet": {
              "type": "string"
            },
            "method": {
              "type": "string"
            },
            "position": {
              "type": "number"
            },
            "reason": {
              "type": "string"
            }
          },
          "required": [
            "id",
            "amount",
            "status",
            "externalId",
            "createdAt"
          ]
        },
        "WithdrawsResponseDto": {
          "type": "object",
          "properties": {
            "items": {
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/WithdrawListResponseDto"
              }
            },
            "total": {
              "type": "number"
            }
          },
          "required": [
            "items",
            "total"
          ]
        },
        "WithdrawMethodResponseDto": {
          "type": "object",
          "properties": {
            "id": {
              "type": "number"
            },
            "name": {
              "type": "string"
            },
            "method": {
              "type": "string"
            },
            "min": {
              "type": "number"
            },
            "max": {
              "type": "number"
            },
            "fee": {
              "type": "number"
            },
            "icon": {
              "type": "string"
            },
            "placeholder": {
              "type": "string"
            },
            "fixedFee": {
              "type": "string"
            },
            "payload": {
              "type": "object"
            }
          },
          "required": [
            "id",
            "name",
            "method",
            "min",
            "max",
            "fee"
          ]
        },
        "WithdrawLimitsResponseDto": {
          "type": "object",
          "properties": {
            "bankCard": {
              "type": "number"
            },
            "crypto": {
              "type": "number"
            }
          },
          "required": [
            "bankCard",
            "crypto"
          ]
        },
        "CreateFileRequestDto": {
          "type": "object",
          "properties": {
            "name": {
              "type": "string",
              "example": "image.jpg"
            }
          },
          "required": [
            "name"
          ]
        },
        "FilePresignedUrlResponseDto": {
          "type": "object",
          "properties": {
            "url": {
              "type": "string"
            },
            "key": {
              "type": "string"
            },
            "contentDisposition": {
              "type": "string",
              "enum": [
                "inline",
                "attachment"
              ]
            }
          },
          "required": [
            "url",
            "key",
            "contentDisposition"
          ]
        },
        "SaveFileRequestDto": {
          "type": "object",
          "properties": {
            "key": {
              "type": "string"
            },
            "name": {
              "type": "string"
            }
          },
          "required": [
            "key",
            "name"
          ]
        },
        "FileResponseDto": {
          "type": "object",
          "properties": {
            "id": {
              "type": "number"
            },
            "name": {
              "type": "string"
            },
            "url": {
              "type": "string"
            },
            "mime": {
              "type": "string"
            }
          },
          "required": [
            "id",
            "name",
            "url",
            "mime"
          ]
        },
        "SessionListResponseDto": {
          "type": "object",
          "properties": {
            "id": {
              "type": "number"
            },
            "agent": {
              "type": "string",
              "example": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/111.0.0.0 Safari/537.36"
            },
            "createdAt": {
              "format": "date-time",
              "type": "string"
            },
            "isCurrent": {
              "type": "boolean"
            },
            "ip": {
              "type": "string",
              "example": "127.0.0.1"
            }
          },
          "required": [
            "id",
            "agent",
            "createdAt",
            "isCurrent",
            "ip"
          ]
        },
        "SessionsResponseDto": {
          "type": "object",
          "properties": {
            "items": {
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/SessionListResponseDto"
              }
            },
            "total": {
              "type": "number"
            }
          },
          "required": [
            "items",
            "total"
          ]
        },
        "AuthChallengeResponseDto": {
          "type": "object",
          "properties": {
            "challenge": {
              "type": "string",
              "description": "pass it to /auth/use-challenge"
            },
            "requirements": {
              "type": "array",
              "description": "these requirements must be issued before you log in to your account",
              "items": {
                "type": "string",
                "enum": [
                  "tfa",
                  "tfa_mail",
                  "banned"
                ]
              }
            }
          },
          "required": [
            "challenge",
            "requirements"
          ]
        },
        "UseBackupCodeRequestDto": {
          "type": "object",
          "properties": {
            "challenge": {
              "type": "string"
            },
            "code": {
              "type": "string",
              "description": "backup code"
            }
          },
          "required": [
            "challenge",
            "code"
          ]
        },
        "UseTotpRequestDto": {
          "type": "object",
          "properties": {
            "challenge": {
              "type": "string"
            },
            "code": {
              "type": "string",
              "description": "totp code"
            }
          },
          "required": [
            "challenge",
            "code"
          ]
        },
        "UseChallengeRequestDto": {
          "type": "object",
          "properties": {
            "challenge": {
              "type": "string"
            }
          },
          "required": [
            "challenge"
          ]
        },
        "ConsentType": {
          "type": "string",
          "enum": [
            "privacy",
            "personal_data",
            "marketing",
            "cookies",
            "recurrent_payments",
            "terms"
          ]
        },
        "ConsentItemDto": {
          "type": "object",
          "properties": {
            "consentType": {
              "allOf": [
                {
                  "$ref": "#/components/schemas/ConsentType"
                }
              ]
            },
            "agreed": {
              "type": "boolean"
            }
          },
          "required": [
            "consentType",
            "agreed"
          ]
        },
        "ConsentSource": {
          "type": "string",
          "enum": [
            "landing_banner",
            "lk_modal",
            "registration",
            "admin_manual",
            "email_unsubscribe"
          ]
        },
        "CreateConsentsDto": {
          "type": "object",
          "properties": {
            "consents": {
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/ConsentItemDto"
              }
            },
            "source": {
              "allOf": [
                {
                  "$ref": "#/components/schemas/ConsentSource"
                }
              ]
            }
          },
          "required": [
            "consents",
            "source"
          ]
        },
        "ConsentCreatedResponseDto": {
          "type": "object",
          "properties": {
            "id": {
              "type": "number"
            },
            "createdAt": {
              "type": "string"
            }
          },
          "required": [
            "id",
            "createdAt"
          ]
        },
        "AccountConsentsResponseDto": {
          "type": "object",
          "properties": {
            "type": {
              "allOf": [
                {
                  "$ref": "#/components/schemas/ConsentType"
                }
              ]
            },
            "agreed": {
              "type": "boolean"
            },
            "version": {
              "type": "string",
              "nullable": true
            },
            "currentVersion": {
              "type": "string",
              "nullable": true,
              "description": "Actual version of the document"
            },
            "isOutdated": {
              "type": "boolean",
              "description": "Consent is outdated (exists a newer version of the document)"
            },
            "date": {
              "type": "string",
              "nullable": true,
              "description": "Date the consent was given; null if no decision recorded"
            },
            "documentUrl": {
              "type": "string",
              "nullable": true
            }
          },
          "required": [
            "type",
            "agreed",
            "version",
            "currentVersion",
            "isOutdated",
            "date",
            "documentUrl"
          ]
        },
        "RevokeConsentDto": {
          "type": "object",
          "properties": {
            "consentType": {
              "allOf": [
                {
                  "$ref": "#/components/schemas/ConsentType"
                }
              ]
            },
            "source": {
              "default": "email_unsubscribe",
              "allOf": [
                {
                  "$ref": "#/components/schemas/ConsentSource"
                }
              ]
            }
          },
          "required": [
            "consentType"
          ]
        },
        "PaymentMethodDetailsResponseDto": {
          "type": "object",
          "properties": {
            "id": {
              "type": "object"
            },
            "accountPaymentMethodId": {
              "type": "object"
            },
            "displayName": {
              "type": "string"
            },
            "issuerCountryAlpha2": {
              "type": "string"
            },
            "expiresAt": {
              "format": "date-time",
              "type": "string",
              "nullable": true
            },
            "createdAt": {
              "format": "date-time",
              "type": "string"
            },
            "updatedAt": {
              "format": "date-time",
              "type": "string"
            }
          },
          "required": [
            "id",
            "accountPaymentMethodId",
            "displayName",
            "issuerCountryAlpha2",
            "expiresAt",
            "createdAt",
            "updatedAt"
          ]
        },
        "AccountPaymentMethodResponseDto": {
          "type": "object",
          "properties": {
            "id": {
              "type": "object"
            },
            "status": {
              "type": "string",
              "description": "Payment method status",
              "enum": [
                "created",
                "active",
                "declined",
                "deleted"
              ]
            },
            "paymentMethodSlug": {
              "type": "string"
            },
            "accountId": {
              "type": "object"
            },
            "detailsId": {
              "type": "object",
              "nullable": true
            },
            "details": {
              "nullable": true,
              "type": "object",
              "allOf": [
                {
                  "$ref": "#/components/schemas/PaymentMethodDetailsResponseDto"
                }
              ]
            },
            "declineReason": {
              "type": "string",
              "enum": [
                "3d_secure_failed",
                "call_issuer",
                "canceled_by_merchant",
                "card_expired",
                "country_forbidden",
                "deal_expired",
                "expired_on_capture",
                "expired_on_confirmation",
                "fraud_suspected",
                "general_decline",
                "identification_required",
                "insufficient_funds",
                "internal_timeout",
                "invalid_card_number",
                "invalid_csc",
                "issuer_unavailable",
                "payment_method_limit_exceeded",
                "payment_method_restricted",
                "permission_revoked",
                "unsupported_mobile_operator",
                "unknown"
              ],
              "nullable": true
            },
            "createdAt": {
              "format": "date-time",
              "type": "string"
            },
            "updatedAt": {
              "format": "date-time",
              "type": "string"
            }
          },
          "required": [
            "id",
            "status",
            "paymentMethodSlug",
            "accountId",
            "detailsId",
            "details",
            "declineReason",
            "createdAt",
            "updatedAt"
          ]
        },
        "AccountPaymentMethodsListResponseDto": {
          "type": "object",
          "properties": {
            "items": {
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/AccountPaymentMethodResponseDto"
              }
            },
            "total": {
              "type": "number"
            }
          },
          "required": [
            "items",
            "total"
          ]
        },
        "CreatePaymentMethodRequestDto": {
          "type": "object",
          "properties": {
            "paymentMethodSlug": {
              "type": "string",
              "example": "yookassa:bank_card"
            }
          },
          "required": [
            "paymentMethodSlug"
          ]
        },
        "CreateAccountPaymentMethodResponseDto": {
          "type": "object",
          "properties": {
            "accountPaymentMethod": {
              "$ref": "#/components/schemas/AccountPaymentMethodResponseDto"
            },
            "confirmationUrl": {
              "type": "string",
              "nullable": true
            }
          },
          "required": [
            "accountPaymentMethod",
            "confirmationUrl"
          ]
        },
        "PaymentMethodGroupResponseDto": {
          "type": "object",
          "properties": {
            "slug": {
              "type": "string",
              "description": "Payment group identifier",
              "example": "card",
              "enum": [
                "card",
                "online",
                "wallet",
                "crypto"
              ]
            },
            "name": {
              "type": "string",
              "description": "Localized group name",
              "example": "Банковские карты"
            }
          },
          "required": [
            "slug",
            "name"
          ]
        },
        "PaymentMethodResponseDto": {
          "type": "object",
          "properties": {
            "slug": {
              "type": "string",
              "description": "Payment method identifier (provider:method format)",
              "example": "cryptobot:usdt"
            },
            "name": {
              "type": "string",
              "description": "Localized payment method name",
              "example": "USDT (CryptoBot)"
            },
            "icon": {
              "type": "string",
              "description": "Icon URL",
              "example": "https://io.aeza.net/icons/payment/usdt.svg"
            },
            "payload": {
              "type": "object",
              "description": "Provider-specific configuration"
            },
            "min": {
              "type": "number",
              "description": "Minimum payment amount in cents",
              "example": 100
            },
            "max": {
              "type": "number",
              "description": "Maximum payment amount in cents",
              "example": 200000,
              "nullable": true
            },
            "order": {
              "type": "number",
              "description": "Display order",
              "example": 1
            },
            "group": {
              "type": "string",
              "description": "Payment group slug",
              "example": "crypto",
              "enum": [
                "card",
                "online",
                "wallet",
                "crypto"
              ]
            },
            "fee": {
              "type": "number",
              "description": "Fee percentage",
              "example": 5
            }
          },
          "required": [
            "slug",
            "name",
            "icon",
            "payload",
            "min",
            "order",
            "group",
            "fee"
          ]
        },
        "PaymentMethodsListResponseDto": {
          "type": "object",
          "properties": {
            "groups": {
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/PaymentMethodGroupResponseDto"
              }
            },
            "items": {
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/PaymentMethodResponseDto"
              }
            },
            "total": {
              "type": "number",
              "description": "Total number of available payment methods",
              "example": 15
            }
          },
          "required": [
            "groups",
            "items",
            "total"
          ]
        },
        "PaymentMethodExchangeRatesListResponseDto": {
          "type": "object",
          "properties": {
            "items": {
              "type": "array",
              "items": {
                "type": "string"
              }
            }
          },
          "required": [
            "items"
          ]
        },
        "EditApiKeyDataRequestDto": {
          "type": "object",
          "properties": {
            "name": {
              "type": "string",
              "minimum": 3,
              "maximum": 32
            },
            "ips": {
              "description": "ip or CIDR (ipv4 and ipv6)",
              "type": "array",
              "items": {
                "type": "string"
              }
            },
            "isActive": {
              "type": "boolean"
            }
          }
        },
        "ApiKeyCreatedResponseDto": {
          "type": "object",
          "properties": {
            "id": {
              "type": "number"
            },
            "ownerId": {
              "type": "number"
            },
            "name": {
              "type": "string"
            },
            "ips": {
              "type": "array",
              "items": {
                "type": "string"
              }
            },
            "isActive": {
              "type": "boolean"
            },
            "lastUsedAt": {
              "format": "date-time",
              "type": "string"
            },
            "lastIp": {
              "type": "string"
            },
            "createdAt": {
              "format": "date-time",
              "type": "string"
            },
            "isLegacy": {
              "type": "boolean"
            },
            "token": {
              "type": "string"
            }
          },
          "required": [
            "id",
            "ownerId",
            "name",
            "isActive",
            "createdAt",
            "isLegacy",
            "token"
          ]
        },
        "ApiKeyResponseDto": {
          "type": "object",
          "properties": {
            "id": {
              "type": "number"
            },
            "ownerId": {
              "type": "number"
            },
            "name": {
              "type": "string"
            },
            "ips": {
              "type": "array",
              "items": {
                "type": "string"
              }
            },
            "isActive": {
              "type": "boolean"
            },
            "lastUsedAt": {
              "format": "date-time",
              "type": "string"
            },
            "lastIp": {
              "type": "string"
            },
            "createdAt": {
              "format": "date-time",
              "type": "string"
            },
            "isLegacy": {
              "type": "boolean"
            }
          },
          "required": [
            "id",
            "ownerId",
            "name",
            "isActive",
            "createdAt",
            "isLegacy"
          ]
        },
        "ApiKeyListResponseDto": {
          "type": "object",
          "properties": {
            "items": {
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/ApiKeyResponseDto"
              }
            },
            "total": {
              "type": "number"
            }
          },
          "required": [
            "items",
            "total"
          ]
        },
        "SsoInitRequestDto": {
          "type": "object",
          "properties": {
            "providerType": {
              "type": "string",
              "enum": [
                "dev",
                "yandex",
                "google",
                "github",
                "apple",
                "telegram",
                "discord"
              ]
            },
            "intent": {
              "type": "string",
              "enum": [
                "registration",
                "login",
                "link"
              ]
            }
          },
          "required": [
            "providerType",
            "intent"
          ]
        },
        "SsoInitResponseDto": {
          "type": "object",
          "properties": {
            "url": {
              "type": "string"
            }
          },
          "required": [
            "url"
          ]
        },
        "SsoConfirmRequestDto": {
          "type": "object",
          "properties": {
            "providerType": {
              "type": "string",
              "enum": [
                "dev",
                "yandex",
                "google",
                "github",
                "apple",
                "telegram",
                "discord"
              ]
            },
            "query": {
              "type": "string",
              "example": "a=foo&e=bar&z=foo&a=bar",
              "description": "POST /auth/sso > SSO > CALLBACK_URL?query"
            }
          },
          "required": [
            "providerType",
            "query"
          ]
        },
        "SsoConfirmRegisterRequestDto": {
          "type": "object",
          "properties": {
            "providerType": {
              "type": "string",
              "enum": [
                "dev",
                "yandex",
                "google",
                "github",
                "apple",
                "telegram",
                "discord"
              ]
            },
            "query": {
              "type": "string",
              "example": "a=foo&e=bar&z=foo&a=bar",
              "description": "POST /auth/sso > SSO > CALLBACK_URL?query"
            },
            "lang": {
              "type": "string",
              "example": "en"
            },
            "referralCode": {
              "type": "string"
            },
            "acceptNewsletter": {
              "type": "boolean"
            }
          },
          "required": [
            "providerType",
            "query",
            "lang"
          ]
        },
        "SsoLinkListResponseDto": {
          "type": "object",
          "properties": {
            "providerType": {
              "type": "string",
              "enum": [
                "dev",
                "yandex",
                "google",
                "github",
                "apple",
                "telegram",
                "discord"
              ]
            },
            "providerId": {
              "type": "string"
            }
          },
          "required": [
            "providerType",
            "providerId"
          ]
        },
        "SsoLinksResponseDto": {
          "type": "object",
          "properties": {
            "items": {
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/SsoLinkListResponseDto"
              }
            },
            "total": {
              "type": "number"
            }
          },
          "required": [
            "items",
            "total"
          ]
        },
        "OAuthClientInfoResponseDto": {
          "type": "object",
          "properties": {
            "name": {
              "type": "string",
              "example": "Sample OAuth Client"
            },
            "avatarUrl": {
              "type": "string",
              "example": "https://cataas.com/cat/cute"
            }
          },
          "required": [
            "name",
            "avatarUrl"
          ]
        },
        "OAuthAuthorizeRequestDto": {
          "type": "object",
          "properties": {
            "clientId": {
              "type": "string",
              "example": "client_id_123"
            },
            "responseType": {
              "type": "string",
              "description": "OAuth response type",
              "enum": [
                "code",
                "token",
                "id_token",
                "code token",
                "code id_token",
                "token id_token",
                "code token id_token"
              ]
            },
            "redirectUri": {
              "type": "string",
              "example": "https://example.com/callback"
            },
            "scope": {
              "type": "string",
              "example": "user:read user:write"
            },
            "state": {
              "type": "string",
              "example": "random_state_string"
            }
          },
          "required": [
            "clientId",
            "responseType",
            "redirectUri"
          ]
        },
        "OAuthAuthorizeResponseDto": {
          "type": "object",
          "properties": {
            "redirectUrl": {
              "type": "string",
              "example": "https://example.com/callback?code=auth_code_123&state=random_state"
            }
          },
          "required": [
            "redirectUrl"
          ]
        },
        "NotificationResponseDto": {
          "type": "object",
          "properties": {
            "id": {
              "type": "number",
              "description": "Notification ID"
            },
            "text": {
              "type": "string",
              "description": "Formatted notification text"
            },
            "isRead": {
              "type": "boolean",
              "description": "Read status"
            },
            "createdAt": {
              "format": "date-time",
              "type": "string",
              "description": "Creation date"
            }
          },
          "required": [
            "id",
            "text",
            "isRead",
            "createdAt"
          ]
        },
        "NotificationListResponseDto": {
          "type": "object",
          "properties": {
            "items": {
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/NotificationResponseDto"
              }
            },
            "total": {
              "type": "number"
            }
          },
          "required": [
            "items",
            "total"
          ]
        },
        "TicketListResponseDto": {
          "type": "object",
          "properties": {
            "id": {
              "type": "number"
            },
            "name": {
              "type": "string",
              "example": "my server is dancing macarena"
            },
            "unreadCount": {
              "type": "number"
            },
            "serviceId": {
              "type": "number"
            },
            "paidTaskId": {
              "type": "number",
              "nullable": true
            }
          },
          "required": [
            "id",
            "name",
            "unreadCount",
            "paidTaskId"
          ]
        },
        "TicketsResponseDto": {
          "type": "object",
          "properties": {
            "open": {
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/TicketListResponseDto"
              }
            },
            "closed": {
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/TicketListResponseDto"
              }
            },
            "solved": {
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/TicketListResponseDto"
              }
            },
            "gpt": {
              "$ref": "#/components/schemas/TicketListResponseDto"
            },
            "totalUnread": {
              "type": "number"
            }
          },
          "required": [
            "open",
            "closed",
            "solved",
            "totalUnread"
          ]
        },
        "CreateTicketRequestDto": {
          "type": "object",
          "properties": {
            "body": {
              "type": "string",
              "example": "I want to file a complaint that I cant find a better hosting service than yours",
              "minimum": 3,
              "maximum": 4096
            },
            "fileIds": {
              "type": "array",
              "items": {
                "type": "number"
              }
            },
            "name": {
              "type": "string",
              "example": "hello moderators",
              "minimum": 3,
              "maximum": 64
            },
            "serviceId": {
              "type": "number"
            },
            "paidTaskId": {
              "type": "number"
            }
          },
          "required": [
            "body",
            "name"
          ]
        },
        "PaymentTerm": {
          "type": "string",
          "enum": [
            "hour",
            "half_day",
            "day",
            "week",
            "month",
            "quarter_year",
            "half_year",
            "year",
            "eternal"
          ]
        },
        "ServiceStatus": {
          "type": "string",
          "enum": [
            "activation_wait",
            "active",
            "suspended",
            "prolong_wait",
            "deleted",
            "blocked",
            "rescue"
          ]
        },
        "ProductNestedResponseDto": {
          "type": "object",
          "properties": {
            "id": {
              "type": "number"
            },
            "name": {
              "type": "string"
            },
            "typeSlug": {
              "type": "string"
            },
            "typeName": {
              "type": "string"
            },
            "groupId": {
              "type": "number"
            },
            "fullPayload": {
              "type": "object"
            },
            "payload": {
              "type": "object"
            },
            "prices": {
              "type": "object"
            },
            "localedPayload": {
              "type": "object"
            },
            "tags": {
              "type": "array",
              "items": {
                "type": "string"
              }
            }
          },
          "required": [
            "id",
            "name",
            "typeSlug",
            "typeName",
            "groupId",
            "fullPayload",
            "payload",
            "prices",
            "localedPayload",
            "tags"
          ]
        },
        "TaskStatus": {
          "type": "string",
          "enum": [
            "queued",
            "running",
            "failed",
            "success",
            "cancelled",
            "wait_child",
            "manual"
          ]
        },
        "ServiceTaskResponseDto": {
          "type": "object",
          "properties": {
            "id": {
              "type": "string"
            },
            "slug": {
              "type": "string"
            },
            "name": {
              "type": "string"
            },
            "createdAt": {
              "format": "date-time",
              "type": "string"
            },
            "status": {
              "allOf": [
                {
                  "$ref": "#/components/schemas/TaskStatus"
                }
              ]
            }
          },
          "required": [
            "id",
            "slug",
            "name",
            "createdAt",
            "status"
          ]
        },
        "ProcessingModuleCapability": {
          "type": "string",
          "enum": [
            "order_many",
            "ctl",
            "ctl.restart",
            "ctl.force_off",
            "ip",
            "manual_delete",
            "upgrade",
            "charts",
            "rename",
            "change_password",
            "reinstall",
            "backups",
            "clone",
            "remote",
            "remote.vnc",
            "rescue",
            "goto",
            "ssh_keys",
            "node",
            "resources_check"
          ]
        },
        "ServiceResponseDto": {
          "type": "object",
          "properties": {
            "id": {
              "type": "number"
            },
            "name": {
              "type": "string",
              "example": "My white rabbit"
            },
            "ip": {
              "type": "string",
              "example": "127.0.0.1"
            },
            "payload": {
              "type": "object"
            },
            "price": {
              "type": "number"
            },
            "paymentTerm": {
              "example": "month",
              "allOf": [
                {
                  "$ref": "#/components/schemas/PaymentTerm"
                }
              ]
            },
            "autoProlong": {
              "type": "boolean"
            },
            "createdAt": {
              "format": "date-time",
              "type": "string"
            },
            "expiresAt": {
              "format": "date-time",
              "type": "string"
            },
            "status": {
              "allOf": [
                {
                  "$ref": "#/components/schemas/ServiceStatus"
                }
              ]
            },
            "typeSlug": {
              "type": "string",
              "example": "vps"
            },
            "productName": {
              "type": "string",
              "example": "SWE-PROMO"
            },
            "product": {
              "$ref": "#/components/schemas/ProductNestedResponseDto"
            },
            "locationCode": {
              "type": "string",
              "example": "de",
              "nullable": true
            },
            "bundle": {
              "type": "string",
              "example": "reseller"
            },
            "currentTask": {
              "$ref": "#/components/schemas/ServiceTaskResponseDto"
            },
            "capabilities": {
              "example": [
                "change_password"
              ],
              "allOf": [
                {
                  "$ref": "#/components/schemas/ProcessingModuleCapability"
                }
              ]
            },
            "ownerId": {
              "type": "number"
            },
            "productId": {
              "type": "number"
            },
            "backups": {
              "type": "boolean"
            },
            "schedule": {
              "type": "object"
            },
            "parameters": {
              "type": "object",
              "description": "Summary parameters (product.parameters + service.additionalResources + service.parameters)"
            },
            "secureParameters": {
              "type": "object"
            }
          },
          "required": [
            "id",
            "name",
            "ip",
            "payload",
            "price",
            "paymentTerm",
            "autoProlong",
            "createdAt",
            "expiresAt",
            "status",
            "typeSlug",
            "productName",
            "product",
            "locationCode",
            "currentTask",
            "capabilities",
            "ownerId",
            "productId",
            "backups",
            "schedule",
            "parameters",
            "secureParameters"
          ]
        },
        "TicketAbuseResponseDto": {
          "type": "object",
          "properties": {
            "status": {
              "type": "string",
              "enum": [
                "open",
                "stopped",
                "performed"
              ]
            },
            "restriction": {
              "type": "string",
              "enum": [
                "denyweb",
                "denyall",
                "block"
              ]
            },
            "startedAt": {
              "format": "date-time",
              "type": "string",
              "nullable": true
            },
            "expiresAt": {
              "format": "date-time",
              "type": "string",
              "nullable": true
            }
          }
        },
        "PaidTaskTicketSummaryDto": {
          "type": "object",
          "properties": {
            "id": {
              "type": "number"
            },
            "status": {
              "type": "string",
              "enum": [
                "draft",
                "created",
                "cancelled",
                "confirmed",
                "in_progress",
                "waiting_approval",
                "done"
              ]
            },
            "title": {
              "type": "string"
            },
            "price": {
              "type": "number"
            },
            "deadlineAt": {
              "format": "date-time",
              "type": "string",
              "nullable": true
            }
          },
          "required": [
            "id",
            "status",
            "title",
            "price",
            "deadlineAt"
          ]
        },
        "TicketResponseDto": {
          "type": "object",
          "properties": {
            "id": {
              "type": "number"
            },
            "name": {
              "type": "string",
              "example": "my server is dancing macarena"
            },
            "status": {
              "type": "string",
              "enum": [
                "open",
                "closed",
                "solved"
              ]
            },
            "service": {
              "$ref": "#/components/schemas/ServiceResponseDto"
            },
            "abuse": {
              "$ref": "#/components/schemas/TicketAbuseResponseDto"
            },
            "rate": {
              "type": "number"
            },
            "rateComment": {
              "type": "string"
            },
            "totalTips": {
              "type": "number"
            },
            "paidTaskId": {
              "type": "number",
              "nullable": true
            },
            "paidTask": {
              "$ref": "#/components/schemas/PaidTaskTicketSummaryDto"
            }
          },
          "required": [
            "id",
            "name",
            "status",
            "paidTaskId"
          ]
        },
        "TicketMessageAuthorResponseDto": {
          "type": "object",
          "properties": {
            "name": {
              "type": "string"
            },
            "photo": {
              "type": "string"
            },
            "isIntern": {
              "type": "boolean"
            },
            "id": {
              "type": "number"
            }
          },
          "required": [
            "name",
            "photo"
          ]
        },
        "TicketMessageListResponseDto": {
          "type": "object",
          "properties": {
            "id": {
              "type": "number"
            },
            "body": {
              "type": "string"
            },
            "createdAt": {
              "format": "date-time",
              "type": "string"
            },
            "role": {
              "type": "string",
              "enum": [
                "client",
                "support"
              ]
            },
            "reaction": {
              "type": "string",
              "enum": [
                "like",
                "dislike",
                "none"
              ]
            },
            "files": {
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/FileResponseDto"
              }
            },
            "author": {
              "$ref": "#/components/schemas/TicketMessageAuthorResponseDto"
            },
            "type": {
              "type": "string",
              "example": "rate"
            },
            "paidTask": {
              "$ref": "#/components/schemas/PaidTaskTicketSummaryDto"
            }
          },
          "required": [
            "id",
            "body",
            "createdAt",
            "role",
            "reaction",
            "files",
            "author"
          ]
        },
        "TicketMessagesResponseDto": {
          "type": "object",
          "properties": {
            "items": {
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/TicketMessageListResponseDto"
              }
            },
            "total": {
              "type": "number"
            }
          },
          "required": [
            "items",
            "total"
          ]
        },
        "SendMessageRequestDto": {
          "type": "object",
          "properties": {
            "body": {
              "type": "string",
              "example": "I want to file a complaint that I cant find a better hosting service than yours",
              "minimum": 3,
              "maximum": 4096
            },
            "fileIds": {
              "type": "array",
              "items": {
                "type": "number"
              }
            }
          },
          "required": [
            "body"
          ]
        },
        "SetMessageReactionRequestDto": {
          "type": "object",
          "properties": {
            "reaction": {
              "type": "string",
              "enum": [
                "like",
                "dislike",
                "none"
              ]
            }
          },
          "required": [
            "reaction"
          ]
        },
        "TicketRateResponseDto": {
          "type": "object",
          "properties": {
            "value": {
              "type": "number"
            },
            "comment": {
              "type": "string"
            }
          }
        },
        "TicketRateRequestDto": {
          "type": "object",
          "properties": {
            "value": {
              "type": "number",
              "minimum": 0,
              "maximum": 4
            },
            "comment": {
              "type": "string"
            }
          },
          "required": [
            "value"
          ]
        },
        "TicketTipsResponseDto": {
          "type": "object",
          "properties": {
            "items": {
              "type": "array",
              "items": {
                "type": "string"
              }
            },
            "total": {
              "type": "number"
            }
          },
          "required": [
            "items",
            "total"
          ]
        },
        "TicketTipRequestDto": {
          "type": "object",
          "properties": {
            "amount": {
              "type": "number",
              "minimum": 0,
              "maximum": 4
            },
            "targetId": {
              "type": "number",
              "description": "employee id"
            }
          },
          "required": [
            "amount"
          ]
        },
        "TicketTipsListResponseDto": {
          "type": "object",
          "properties": {
            "target": {
              "type": "object"
            },
            "amount": {
              "type": "number"
            },
            "createdAt": {
              "format": "date-time",
              "type": "string"
            }
          },
          "required": [
            "amount",
            "createdAt"
          ]
        },
        "SendGptMessageRequestDto": {
          "type": "object",
          "properties": {
            "body": {
              "type": "string",
              "example": "How to crack aeza.net?",
              "minimum": 3,
              "maximum": 512
            }
          },
          "required": [
            "body"
          ]
        },
        "ServiceSmallResponseDto": {
          "type": "object",
          "properties": {
            "id": {
              "type": "number"
            },
            "name": {
              "type": "string",
              "example": "My white rabbit"
            },
            "ip": {
              "type": "string",
              "example": "127.0.0.1"
            },
            "payload": {
              "type": "object"
            },
            "price": {
              "type": "number"
            },
            "paymentTerm": {
              "example": "month",
              "allOf": [
                {
                  "$ref": "#/components/schemas/PaymentTerm"
                }
              ]
            },
            "autoProlong": {
              "type": "boolean"
            },
            "createdAt": {
              "format": "date-time",
              "type": "string"
            },
            "expiresAt": {
              "format": "date-time",
              "type": "string"
            },
            "status": {
              "allOf": [
                {
                  "$ref": "#/components/schemas/ServiceStatus"
                }
              ]
            },
            "typeSlug": {
              "type": "string",
              "example": "vps"
            },
            "productName": {
              "type": "string",
              "example": "SWE-PROMO"
            },
            "product": {
              "$ref": "#/components/schemas/ProductNestedResponseDto"
            },
            "locationCode": {
              "type": "string",
              "example": "de",
              "nullable": true
            },
            "bundle": {
              "type": "string",
              "example": "reseller"
            },
            "currentTask": {
              "$ref": "#/components/schemas/ServiceTaskResponseDto"
            },
            "capabilities": {
              "example": [
                "change_password"
              ],
              "allOf": [
                {
                  "$ref": "#/components/schemas/ProcessingModuleCapability"
                }
              ]
            }
          },
          "required": [
            "id",
            "name",
            "ip",
            "payload",
            "price",
            "paymentTerm",
            "autoProlong",
            "createdAt",
            "expiresAt",
            "status",
            "typeSlug",
            "productName",
            "product",
            "locationCode",
            "currentTask",
            "capabilities"
          ]
        },
        "ServicesListResponseDto": {
          "type": "object",
          "properties": {
            "items": {
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/ServiceSmallResponseDto"
              }
            },
            "total": {
              "type": "number"
            }
          },
          "required": [
            "items",
            "total"
          ]
        },
        "ServiceTermPriceMapResponseDto": {
          "type": "object",
          "properties": {
            "hour": {
              "type": "number",
              "example": 100
            },
            "half_day": {
              "type": "number",
              "example": 100
            },
            "day": {
              "type": "number",
              "example": 100
            },
            "week": {
              "type": "number",
              "example": 100
            },
            "month": {
              "type": "number",
              "example": 100
            },
            "quarter_year": {
              "type": "number",
              "example": 100
            },
            "half_year": {
              "type": "number",
              "example": 100
            },
            "year": {
              "type": "number",
              "example": 100
            },
            "eternal": {
              "type": "number",
              "example": 100
            }
          }
        },
        "ServiceTermPricesResponseDto": {
          "type": "object",
          "properties": {
            "prices": {
              "$ref": "#/components/schemas/ServiceTermPriceMapResponseDto"
            }
          },
          "required": [
            "prices"
          ]
        },
        "ProlongServiceRequestDto": {
          "type": "object",
          "properties": {
            "method": {
              "type": "string",
              "description": "payment method to use",
              "example": "balance"
            },
            "paymentTerm": {
              "description": "payment period unit. Combined with count determines the total prolongation duration.",
              "example": "month",
              "allOf": [
                {
                  "$ref": "#/components/schemas/PaymentTerm"
                }
              ]
            },
            "count": {
              "type": "number",
              "description": "number of payment periods. The total prolongation duration equals count multiplied by paymentTerm. Example: paymentTerm=month and count=3 means 3 months.",
              "example": 3,
              "minimum": 1
            }
          },
          "required": [
            "method",
            "paymentTerm",
            "count"
          ]
        },
        "InvoiceResponseDto": {
          "type": "object",
          "properties": {
            "id": {
              "type": "number"
            },
            "flowType": {
              "type": "string",
              "enum": [
                "default",
                "recurrent"
              ]
            },
            "status": {
              "type": "string",
              "enum": [
                "created",
                "performed",
                "cancelled",
                "failed"
              ]
            },
            "amount": {
              "type": "number"
            },
            "scenario": {
              "type": "string",
              "nullable": true
            },
            "externalId": {
              "type": "string"
            },
            "payload": {
              "type": "object"
            },
            "receiptUrl": {
              "type": "string"
            },
            "createdAt": {
              "format": "date-time",
              "type": "string"
            },
            "performedAt": {
              "format": "date-time",
              "type": "string",
              "nullable": true
            }
          },
          "required": [
            "id",
            "flowType",
            "status",
            "amount",
            "externalId",
            "payload",
            "createdAt"
          ]
        },
        "BillingTransactionStatus": {
          "type": "string",
          "enum": [
            "created",
            "performed",
            "cancelled"
          ]
        },
        "BillingTransactionType": {
          "type": "string",
          "enum": [
            "manual",
            "replenishment",
            "prolong",
            "buy",
            "order",
            "change_product",
            "edit_resources",
            "tips",
            "paid_task",
            "buy_gift",
            "compensation",
            "refund",
            "overdraft_comission"
          ]
        },
        "InvoiceNestedResponseDto": {
          "type": "object",
          "properties": {
            "id": {
              "type": "number"
            },
            "flowType": {
              "type": "string",
              "enum": [
                "default",
                "recurrent"
              ]
            },
            "status": {
              "type": "string",
              "enum": [
                "created",
                "performed",
                "cancelled",
                "failed"
              ]
            }
          },
          "required": [
            "id",
            "flowType",
            "status"
          ]
        },
        "BillingTransactionResponseDto": {
          "type": "object",
          "properties": {
            "id": {
              "type": "number"
            },
            "amount": {
              "type": "number"
            },
            "bonusAmount": {
              "type": "number"
            },
            "status": {
              "allOf": [
                {
                  "$ref": "#/components/schemas/BillingTransactionStatus"
                }
              ]
            },
            "performedAt": {
              "format": "date-time",
              "type": "string"
            },
            "createdAt": {
              "format": "date-time",
              "type": "string"
            },
            "type": {
              "allOf": [
                {
                  "$ref": "#/components/schemas/BillingTransactionType"
                }
              ]
            },
            "serviceId": {
              "type": "number"
            },
            "payload": {
              "type": "object"
            },
            "invoiceId": {
              "type": "number",
              "nullable": true
            },
            "invoice": {
              "nullable": true,
              "type": "object",
              "allOf": [
                {
                  "$ref": "#/components/schemas/InvoiceNestedResponseDto"
                }
              ]
            }
          },
          "required": [
            "id",
            "amount",
            "bonusAmount",
            "status",
            "performedAt",
            "createdAt",
            "type",
            "serviceId",
            "payload",
            "invoiceId",
            "invoice"
          ]
        },
        "BillingBuyResponseDto": {
          "type": "object",
          "properties": {
            "invoice": {
              "$ref": "#/components/schemas/InvoiceResponseDto"
            },
            "transactions": {
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/BillingTransactionResponseDto"
              }
            }
          }
        },
        "PriceItemResponseDto": {
          "type": "object",
          "properties": {
            "slug": {
              "type": "string",
              "example": "base"
            },
            "price": {
              "type": "number",
              "example": 100
            },
            "count": {
              "type": "number",
              "example": 1
            }
          },
          "required": [
            "slug",
            "price"
          ]
        },
        "ResourcesAvailabilityResponseDto": {
          "type": "object",
          "properties": {
            "status": {
              "type": "string",
              "enum": [
                "ok",
                "not_enough_resources",
                "not_enough_ips",
                "not_enough_resources_and_ips"
              ]
            },
            "resourcesAvailable": {
              "type": "boolean",
              "description": "Whether vCPU/RAM/disk are available"
            },
            "ipv4Available": {
              "type": "boolean",
              "description": "Whether IPv4 addresses are available"
            },
            "ipv6Available": {
              "type": "boolean",
              "description": "Whether IPv6 addresses are available"
            }
          },
          "required": [
            "status",
            "resourcesAvailable",
            "ipv4Available",
            "ipv6Available"
          ]
        },
        "PriceResponseDto": {
          "type": "object",
          "properties": {
            "totalPrice": {
              "type": "number",
              "example": 100
            },
            "items": {
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/PriceItemResponseDto"
              }
            },
            "discount": {
              "type": "number",
              "example": 0
            },
            "availability": {
              "description": "Resource and IP availability for the configuration",
              "allOf": [
                {
                  "$ref": "#/components/schemas/ResourcesAvailabilityResponseDto"
                }
              ]
            },
            "prolongPrice": {
              "type": "number",
              "example": 0
            }
          },
          "required": [
            "totalPrice",
            "items"
          ]
        },
        "ChangeProductPriceResponseDto": {
          "type": "object",
          "properties": {
            "price": {
              "type": "number",
              "description": "Prorated amount for the remaining period",
              "example": 150
            },
            "totalPriceAfterChange": {
              "type": "number",
              "description": "Full recurring service price after the product change for the current payment term",
              "example": 1188
            }
          },
          "required": [
            "price",
            "totalPriceAfterChange"
          ]
        },
        "ChangeProductRequestDto": {
          "type": "object",
          "properties": {
            "method": {
              "type": "string",
              "description": "payment method to use",
              "example": "balance"
            },
            "productId": {
              "type": "number",
              "description": "Target product id to switch to",
              "example": 42
            }
          },
          "required": [
            "method",
            "productId"
          ]
        },
        "ServiceStatsResponseDto": {
          "type": "object",
          "properties": {
            "data": {
              "type": "array",
              "items": {
                "type": "string"
              }
            }
          },
          "required": [
            "data"
          ]
        },
        "ProductTypesResponseDto": {
          "type": "object",
          "properties": {
            "items": {
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/ProductTypeResponseDto"
              }
            },
            "total": {
              "type": "number"
            }
          },
          "required": [
            "items",
            "total"
          ]
        },
        "BillingTransactionsListResponseDto": {
          "type": "object",
          "properties": {
            "items": {
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/BillingTransactionResponseDto"
              }
            },
            "total": {
              "type": "number"
            }
          },
          "required": [
            "items",
            "total"
          ]
        },
        "CreateInvoiceRequestDto": {
          "type": "object",
          "properties": {
            "method": {
              "type": "string",
              "description": "Payment method slug (provider:method)",
              "example": "stripe:default"
            },
            "amount": {
              "type": "number",
              "description": "Top-up amount in minor currency units (cents)",
              "example": 1000
            }
          },
          "required": [
            "method",
            "amount"
          ]
        },
        "ConfigureRecurrentPaymentsRequestDto": {
          "type": "object",
          "properties": {
            "paymentMethodId": {
              "type": "string",
              "description": "Account payment method id to perform recurrent payments"
            },
            "balanceAmountThreshold": {
              "type": "number",
              "description": "Minimal balance amount to make a payments"
            },
            "paymentAmount": {
              "type": "number",
              "description": "Amount to pay every day until thi account balance wont be greater than or equal to provided balanceAmountThreshold"
            }
          },
          "required": [
            "paymentMethodId",
            "balanceAmountThreshold",
            "paymentAmount"
          ]
        },
        "RecurrentPaymentConfigResponseDto": {
          "type": "object",
          "properties": {
            "id": {
              "type": "number"
            },
            "accountId": {
              "type": "number"
            },
            "paymentMethodId": {
              "type": "string",
              "description": "Account payment method id to perform recurrent payments"
            },
            "balanceAmountThreshold": {
              "type": "number",
              "description": "Minimal balance amount to make a payments"
            },
            "paymentAmount": {
              "type": "number",
              "description": "Amount to pay every day until thi account balance wont be greater than or equal to provided balanceAmountThreshold"
            },
            "createdAt": {
              "format": "date-time",
              "type": "string"
            },
            "updatedAt": {
              "format": "date-time",
              "type": "string"
            }
          },
          "required": [
            "id",
            "accountId",
            "paymentMethodId",
            "balanceAmountThreshold",
            "paymentAmount",
            "createdAt",
            "updatedAt"
          ]
        },
        "UpdateRecurrentPaymentsConfigRequestDto": {
          "type": "object",
          "properties": {
            "paymentMethodId": {
              "type": "string",
              "description": "Account payment method id to perform recurrent payments"
            },
            "balanceAmountThreshold": {
              "type": "number",
              "description": "Minimal balance amount to make a payments"
            },
            "paymentAmount": {
              "type": "number",
              "description": "Amount to pay every day until thi account balance wont be greater than or equal to provided balanceAmountThreshold"
            }
          }
        },
        "Object": {
          "type": "object",
          "properties": {}
        },
        "OrderItemDto": {
          "type": "object",
          "properties": {
            "productId": {
              "type": "number",
              "description": "Product ID",
              "example": 12345
            },
            "count": {
              "type": "number",
              "description": "Number of services to create",
              "example": 1,
              "minimum": 1
            },
            "name": {
              "type": "string",
              "description": "Base service name (used for auto-generation when count > 1)",
              "example": "my-server"
            },
            "term": {
              "type": "string",
              "description": "Payment term",
              "enum": [
                "hour",
                "half_day",
                "day",
                "week",
                "month",
                "quarter_year",
                "half_year",
                "year",
                "eternal"
              ],
              "default": "month"
            },
            "termCount": {
              "type": "number",
              "description": "Multiplier for payment term (e.g., termCount: 24 with term: hour = 24 hours)",
              "example": 1,
              "minimum": 1,
              "default": 1
            },
            "autoProlong": {
              "type": "boolean",
              "description": "Enable auto prolongation",
              "example": true
            },
            "parameters": {
              "type": "object",
              "description": "Service parameters",
              "example": {}
            },
            "additionalResources": {
              "type": "object",
              "description": "Additional resources to upgrade beyond product base values (e.g. extra cpu, ram)",
              "example": {
                "cpu": 4,
                "ram": 8192
              }
            },
            "sshKeyIds": {
              "description": "SSH key IDs to attach on service creation",
              "example": [
                1,
                2
              ],
              "type": "array",
              "items": {
                "type": "number"
              }
            }
          },
          "required": [
            "productId",
            "count",
            "name",
            "autoProlong",
            "parameters"
          ]
        },
        "CreateOrderRequestDto": {
          "type": "object",
          "properties": {
            "method": {
              "type": "string",
              "description": "payment method to use",
              "example": "balance"
            },
            "orders": {
              "description": "Array of service orders",
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/OrderItemDto"
              }
            }
          },
          "required": [
            "method",
            "orders"
          ]
        },
        "ServiceLimitGradeResponseDto": {
          "type": "object",
          "properties": {
            "from": {
              "type": "number"
            },
            "available": {
              "type": "number"
            }
          },
          "required": [
            "from",
            "available"
          ]
        },
        "ServiceLimitResponseDto": {
          "type": "object",
          "properties": {
            "id": {
              "type": "number"
            },
            "slug": {
              "type": "string",
              "example": "limit-1"
            },
            "name": {
              "type": "string"
            },
            "groupIds": {
              "type": "array",
              "items": {
                "type": "number"
              }
            },
            "paymentTerms": {
              "type": "array",
              "items": {
                "type": "string",
                "enum": [
                  "hour",
                  "half_day",
                  "day",
                  "week",
                  "month",
                  "quarter_year",
                  "half_year",
                  "year",
                  "eternal"
                ]
              }
            },
            "grades": {
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/ServiceLimitGradeResponseDto"
              }
            },
            "available": {
              "type": "number"
            },
            "used": {
              "type": "number"
            }
          },
          "required": [
            "id",
            "slug",
            "name",
            "groupIds",
            "paymentTerms",
            "grades",
            "available",
            "used"
          ]
        },
        "ServicesLimitsListResponseDto": {
          "type": "object",
          "properties": {
            "items": {
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/ServiceLimitResponseDto"
              }
            },
            "total": {
              "type": "number"
            }
          },
          "required": [
            "items",
            "total"
          ]
        },
        "SshKeyResponseDto": {
          "type": "object",
          "properties": {
            "id": {
              "type": "number"
            },
            "ownerId": {
              "type": "number"
            },
            "name": {
              "type": "string",
              "example": "My laptop"
            },
            "publicKey": {
              "type": "string",
              "example": "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... user@host"
            },
            "autoAssign": {
              "type": "boolean"
            },
            "createdAt": {
              "format": "date-time",
              "type": "string"
            }
          },
          "required": [
            "id",
            "ownerId",
            "name",
            "publicKey",
            "autoAssign",
            "createdAt"
          ]
        },
        "SshKeyListResponseDto": {
          "type": "object",
          "properties": {
            "items": {
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/SshKeyResponseDto"
              }
            },
            "total": {
              "type": "number"
            }
          },
          "required": [
            "items",
            "total"
          ]
        },
        "CreateSshKeyRequestDto": {
          "type": "object",
          "properties": {
            "name": {
              "type": "string",
              "example": "My laptop",
              "maxLength": 255
            },
            "publicKey": {
              "type": "string",
              "example": "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... user@host",
              "description": "OpenSSH public key"
            },
            "autoAssign": {
              "type": "boolean",
              "example": true
            }
          },
          "required": [
            "name",
            "publicKey",
            "autoAssign"
          ]
        },
        "EditSshKeyRequestDto": {
          "type": "object",
          "properties": {
            "name": {
              "type": "string",
              "example": "My laptop",
              "maxLength": 255
            },
            "publicKey": {
              "type": "string",
              "example": "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... user@host",
              "description": "OpenSSH public key"
            },
            "autoAssign": {
              "type": "boolean",
              "example": true
            }
          }
        },
        "IpResponseDto": {
          "type": "object",
          "properties": {
            "key": {
              "type": "string"
            },
            "value": {
              "type": "string"
            },
            "gateway": {
              "type": "string"
            },
            "mask": {
              "type": "string"
            },
            "domain": {
              "type": "string"
            }
          },
          "required": [
            "key",
            "value",
            "gateway",
            "mask",
            "domain"
          ]
        },
        "Ipv4ListResponseDto": {
          "type": "object",
          "properties": {
            "total": {
              "type": "number"
            },
            "items": {
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/IpResponseDto"
              }
            }
          },
          "required": [
            "total",
            "items"
          ]
        },
        "Ipv4PriceResponseDto": {
          "type": "object",
          "properties": {
            "termLimits": {
              "type": "object"
            },
            "price": {
              "type": "number",
              "description": "One-time payment to buy IPv4 for the remaining period"
            },
            "protectedPrice": {
              "type": "number",
              "description": "One-time payment to buy protected IPv4 for the remaining period"
            },
            "periodPrice": {
              "type": "number",
              "description": "Full IPv4 price per payment term"
            },
            "protectedPeriodPrice": {
              "type": "number",
              "description": "Full protected IPv4 price per payment term"
            }
          },
          "required": [
            "termLimits",
            "price",
            "protectedPrice",
            "periodPrice",
            "protectedPeriodPrice"
          ]
        },
        "BuyIpv4RequestDto": {
          "type": "object",
          "properties": {
            "extendedDdosProtection": {
              "type": "boolean"
            },
            "method": {
              "type": "string"
            },
            "domain": {
              "type": "string"
            }
          },
          "required": [
            "method",
            "domain"
          ]
        },
        "DeleteIpv4RequestDto": {
          "type": "object",
          "properties": {
            "key": {
              "type": "number"
            }
          },
          "required": [
            "key"
          ]
        },
        "Ipv6ResponseDto": {
          "type": "object",
          "properties": {
            "key": {
              "type": "string"
            },
            "value": {
              "type": "string"
            },
            "prefix": {
              "type": "number"
            },
            "gateway": {
              "type": "string"
            },
            "ips": {
              "type": "array",
              "items": {
                "type": "string"
              }
            }
          },
          "required": [
            "key",
            "value",
            "prefix",
            "gateway",
            "ips"
          ]
        },
        "Ipv6ListResponseDto": {
          "type": "object",
          "properties": {
            "total": {
              "type": "number"
            },
            "items": {
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/Ipv6ResponseDto"
              }
            }
          },
          "required": [
            "total",
            "items"
          ]
        },
        "ServicesNetworksEditPtrRequestDto": {
          "type": "object",
          "properties": {
            "domain": {
              "type": "string",
              "description": "PTR record",
              "example": "domain.example",
              "maxLength": 128
            }
          },
          "required": [
            "domain"
          ]
        },
        "ServiceResourcesResponseDto": {
          "type": "object",
          "properties": {}
        },
        "GetAdditionalResourcesPriceRequestDto": {
          "type": "object",
          "properties": {
            "additionalResources": {
              "type": "object",
              "description": "New additional resources value relative to base tariff. Key is resource slug, value is new additional resources amount.",
              "example": {
                "ram": 4
              }
            }
          },
          "required": [
            "additionalResources"
          ]
        },
        "GetAdditionalResourcesPriceResponseDto": {
          "type": "object",
          "properties": {
            "oneTimePayment": {
              "type": "number",
              "description": "One-time payment amount for the remaining period",
              "example": 100
            },
            "priceIncrease": {
              "type": "number",
              "description": "Price increase per payment term",
              "example": 500
            },
            "totalPrice": {
              "type": "number",
              "description": "Total price per payment term",
              "example": 1500
            },
            "availability": {
              "description": "Resource and IP availability for the configuration",
              "allOf": [
                {
                  "$ref": "#/components/schemas/ResourcesAvailabilityResponseDto"
                }
              ]
            }
          },
          "required": [
            "oneTimePayment",
            "priceIncrease",
            "totalPrice"
          ]
        },
        "AddAdditionalResourcesRequestDto": {
          "type": "object",
          "properties": {
            "method": {
              "type": "string",
              "description": "payment method to use",
              "example": "balance"
            },
            "additionalResources": {
              "type": "object",
              "description": "New additional resources value relative to base tariff. Key is resource slug, value is new additional resources amount.",
              "example": {
                "ram": 4
              }
            }
          },
          "required": [
            "method",
            "additionalResources"
          ]
        },
        "ServiceCtlForceRequestDto": {
          "type": "object",
          "properties": {
            "force": {
              "type": "boolean"
            }
          }
        },
        "RemoteVncResponseDto": {
          "type": "object",
          "properties": {
            "address": {
              "type": "string",
              "description": "Websocket VNC connection URL",
              "example": "wss://..."
            },
            "password": {
              "type": "string",
              "description": "VNC password",
              "example": "ABCDE12345"
            }
          },
          "required": [
            "address",
            "password"
          ]
        },
        "ReinstallServiceRequestDto": {
          "type": "object",
          "properties": {
            "os": {
              "type": "string",
              "description": "OS slug"
            },
            "recipe": {
              "type": "string",
              "description": "recipe slug"
            },
            "password": {
              "type": "string",
              "description": "new password",
              "minLength": 8,
              "maxLength": 32
            },
            "sshKeyIds": {
              "description": "SSH key ids to inject into the reinstalled server",
              "example": [
                1,
                2
              ],
              "type": "array",
              "items": {
                "type": "number"
              }
            }
          },
          "required": [
            "os",
            "recipe",
            "password"
          ]
        },
        "ServiceBackupSource": {
          "type": "string",
          "enum": [
            "manual",
            "schedule",
            "schedule_old"
          ]
        },
        "ServiceBackupStatus": {
          "type": "string",
          "enum": [
            "creating",
            "active",
            "deleted"
          ]
        },
        "ServiceBackupResponseDto": {
          "type": "object",
          "properties": {
            "id": {
              "type": "number"
            },
            "name": {
              "type": "string"
            },
            "size": {
              "type": "number",
              "nullable": true,
              "description": "Size in gigabytes"
            },
            "createdAt": {
              "format": "date-time",
              "type": "string"
            },
            "source": {
              "allOf": [
                {
                  "$ref": "#/components/schemas/ServiceBackupSource"
                }
              ]
            },
            "status": {
              "allOf": [
                {
                  "$ref": "#/components/schemas/ServiceBackupStatus"
                }
              ]
            }
          },
          "required": [
            "id",
            "name",
            "size",
            "createdAt",
            "source",
            "status"
          ]
        },
        "ServiceBackupListResponseDto": {
          "type": "object",
          "properties": {
            "items": {
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/ServiceBackupResponseDto"
              }
            },
            "total": {
              "type": "number"
            }
          },
          "required": [
            "items",
            "total"
          ]
        },
        "CreateServiceBackupRequestDto": {
          "type": "object",
          "properties": {
            "name": {
              "type": "string"
            }
          },
          "required": [
            "name"
          ]
        },
        "ServiceBackupScheduleType": {
          "type": "string",
          "enum": [
            "daily",
            "weekly",
            "monthly"
          ]
        },
        "SetServiceBackupScheduleRequestDto": {
          "type": "object",
          "properties": {
            "limit": {
              "type": "number",
              "description": "Count of backups to keep",
              "minimum": 1,
              "maximum": 5
            },
            "type": {
              "allOf": [
                {
                  "$ref": "#/components/schemas/ServiceBackupScheduleType"
                }
              ]
            },
            "weekDay": {
              "type": "number",
              "description": "Day of the week to backup",
              "minimum": 1,
              "maximum": 7
            },
            "monthDay": {
              "type": "number",
              "description": "Day of month for backup (if day > month length, backup on last day)",
              "minimum": 1,
              "maximum": 30
            }
          },
          "required": [
            "limit",
            "type"
          ]
        },
        "BillingBuyRequestDto": {
          "type": "object",
          "properties": {
            "method": {
              "type": "string",
              "description": "payment method to use",
              "example": "balance"
            }
          },
          "required": [
            "method"
          ]
        },
        "DomainResponseDto": {
          "type": "object",
          "properties": {
            "id": {
              "type": "number"
            },
            "name": {
              "type": "string",
              "example": "example.com"
            },
            "status": {
              "type": "string",
              "enum": [
                "pending_delegation",
                "active",
                "degraded",
                "archived",
                "deleted"
              ]
            },
            "statusReason": {
              "type": "string",
              "nullable": true
            },
            "observedNameservers": {
              "nullable": true,
              "example": [
                "ns1.aeza-dns.net",
                "ns2.aeza-dns.net"
              ],
              "description": "Nameservers observed during the last delegation check.",
              "type": "array",
              "items": {
                "type": "string"
              }
            },
            "nsCheckScheduledAt": {
              "format": "date-time",
              "type": "string",
              "nullable": true
            },
            "createdAt": {
              "format": "date-time",
              "type": "string"
            },
            "updatedAt": {
              "format": "date-time",
              "type": "string"
            }
          },
          "required": [
            "id",
            "name",
            "status",
            "createdAt",
            "updatedAt"
          ]
        },
        "DomainListResponseDto": {
          "type": "object",
          "properties": {
            "items": {
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/DomainResponseDto"
              }
            },
            "total": {
              "type": "number"
            }
          },
          "required": [
            "items",
            "total"
          ]
        },
        "CreateDomainRequestDto": {
          "type": "object",
          "properties": {
            "name": {
              "type": "string",
              "example": "example.com"
            }
          },
          "required": [
            "name"
          ]
        },
        "DeleteDomainResponseDto": {
          "type": "object",
          "properties": {
            "id": {
              "type": "object"
            },
            "name": {
              "type": "string"
            },
            "status": {
              "type": "string",
              "enum": [
                "pending_delegation",
                "active",
                "degraded",
                "archived",
                "deleted"
              ]
            }
          },
          "required": [
            "id",
            "name",
            "status"
          ]
        },
        "CheckDomainDelegationRequestDto": {
          "type": "object",
          "properties": {
            "name": {
              "type": "string",
              "example": "example.com"
            }
          },
          "required": [
            "name"
          ]
        },
        "CheckDomainDelegationResponseDto": {
          "type": "object",
          "properties": {
            "delegated": {
              "type": "boolean",
              "description": "True when the domain belongs to the caller, is registered, and is fully delegated to our nameservers."
            }
          },
          "required": [
            "delegated"
          ]
        },
        "DomainExpectedNameserversListResponseDto": {
          "type": "object",
          "properties": {
            "items": {
              "example": [
                "ns1.aeza-dns.net",
                "ns2.aeza-dns.net",
                "ns3.aeza-dns.net",
                "ns4.aeza-dns.net"
              ],
              "description": "Expected nameservers for domain zone delegation.",
              "type": "array",
              "items": {
                "type": "string"
              }
            }
          },
          "required": [
            "items"
          ]
        },
        "DomainZoneRecordTypePartResponseDto": {
          "type": "object",
          "properties": {
            "slug": {
              "type": "string",
              "example": "priority"
            },
            "pattern": {
              "type": "string",
              "example": "^\\d+$",
              "description": "Regex filter for the field value."
            },
            "type": {
              "type": "string",
              "enum": [
                "number",
                "string"
              ],
              "example": "number"
            }
          },
          "required": [
            "slug",
            "pattern"
          ]
        },
        "DomainZoneRecordTypeResponseDto": {
          "type": "object",
          "properties": {
            "name": {
              "type": "string",
              "example": "A"
            },
            "description": {
              "type": "string",
              "example": "Address record, matching between name and IP address. Specify IPv4 where the domain or subdomain should lead.",
              "description": "Localized record type description."
            },
            "pattern": {
              "type": "string",
              "example": "^((25[0-5]|(2[0-4]|1\\d|[1-9]|)\\d)(\\.(?!$)|$)){4}$",
              "description": "Regex filter for record content (rdata)."
            },
            "parts": {
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/DomainZoneRecordTypePartResponseDto"
              }
            }
          },
          "required": [
            "name",
            "description",
            "pattern"
          ]
        },
        "DomainZoneRecordTypesListResponseDto": {
          "type": "object",
          "properties": {
            "items": {
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/DomainZoneRecordTypeResponseDto"
              }
            }
          },
          "required": [
            "items"
          ]
        },
        "DomainRecordResponseDto": {
          "type": "object",
          "properties": {
            "id": {
              "type": "number"
            },
            "type": {
              "type": "string",
              "example": "A"
            },
            "name": {
              "type": "string",
              "example": "www"
            },
            "content": {
              "type": "string",
              "example": "192.0.2.1"
            },
            "ttl": {
              "type": "number"
            },
            "priority": {
              "type": "number",
              "nullable": true
            },
            "weight": {
              "type": "number",
              "nullable": true
            },
            "port": {
              "type": "number",
              "nullable": true
            },
            "isEnabled": {
              "type": "boolean"
            },
            "note": {
              "type": "string",
              "nullable": true
            },
            "createdAt": {
              "format": "date-time",
              "type": "string"
            },
            "updatedAt": {
              "format": "date-time",
              "type": "string"
            }
          },
          "required": [
            "id",
            "type",
            "name",
            "content",
            "ttl",
            "isEnabled",
            "createdAt",
            "updatedAt"
          ]
        },
        "DomainRecordListResponseDto": {
          "type": "object",
          "properties": {
            "items": {
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/DomainRecordResponseDto"
              }
            },
            "total": {
              "type": "number"
            }
          },
          "required": [
            "items",
            "total"
          ]
        },
        "CreateDomainRecordRequestDto": {
          "type": "object",
          "properties": {
            "type": {
              "type": "string",
              "example": "A",
              "enum": [
                "A",
                "AAAA",
                "TXT",
                "CNAME",
                "DNAME",
                "ALIAS",
                "MX",
                "SRV"
              ]
            },
            "name": {
              "type": "string",
              "example": "www",
              "description": "Relative owner in zone. Use @ for apex (empty string is normalized to @)."
            },
            "content": {
              "type": "string",
              "example": "192.0.2.1",
              "description": "Record rdata. For A/AAAA — address; for CNAME/NS/MX/PTR/DNAME — FQDN without trailing dot."
            },
            "ttl": {
              "type": "number",
              "default": 3600
            },
            "priority": {
              "type": "number",
              "nullable": true
            },
            "weight": {
              "type": "number",
              "nullable": true
            },
            "port": {
              "type": "number",
              "nullable": true
            },
            "isEnabled": {
              "type": "boolean",
              "default": true
            },
            "note": {
              "type": "string",
              "nullable": true
            }
          },
          "required": [
            "type",
            "name",
            "content"
          ]
        },
        "EditDomainRecordRequestDto": {
          "type": "object",
          "properties": {
            "type": {
              "type": "string",
              "example": "A",
              "enum": [
                "A",
                "AAAA",
                "TXT",
                "CNAME",
                "DNAME",
                "ALIAS",
                "MX",
                "SRV"
              ]
            },
            "name": {
              "type": "string",
              "example": "www",
              "description": "Relative owner in zone. Use @ for apex (empty string is normalized to @)."
            },
            "content": {
              "type": "string",
              "example": "192.0.2.1",
              "description": "Record rdata. For A/AAAA — address; for CNAME/NS/MX/PTR/DNAME — FQDN without trailing dot."
            },
            "ttl": {
              "type": "number"
            },
            "priority": {
              "type": "number",
              "nullable": true
            },
            "weight": {
              "type": "number",
              "nullable": true
            },
            "port": {
              "type": "number",
              "nullable": true
            },
            "isEnabled": {
              "type": "boolean"
            },
            "note": {
              "type": "string",
              "nullable": true,
              "description": "User note only; does not bump zone version for reconcile."
            }
          }
        },
        "GiftListResponseDto": {
          "type": "object",
          "properties": {
            "id": {
              "type": "number"
            },
            "code": {
              "type": "string"
            },
            "amount": {
              "type": "number"
            },
            "comment": {
              "type": "string"
            },
            "isObtained": {
              "type": "boolean"
            },
            "style": {
              "type": "string"
            },
            "hue": {
              "type": "number"
            },
            "emojis": {
              "type": "string"
            }
          },
          "required": [
            "id",
            "code",
            "amount",
            "comment",
            "isObtained",
            "style",
            "hue",
            "emojis"
          ]
        },
        "GiftsResponseDto": {
          "type": "object",
          "properties": {
            "items": {
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/GiftListResponseDto"
              }
            },
            "total": {
              "type": "number"
            }
          },
          "required": [
            "items",
            "total"
          ]
        },
        "BuyGiftRequestDto": {
          "type": "object",
          "properties": {
            "amount": {
              "type": "number"
            },
            "method": {
              "type": "string"
            },
            "comment": {
              "type": "string"
            },
            "style": {
              "type": "string"
            },
            "hue": {
              "type": "number"
            },
            "emojis": {
              "type": "string"
            }
          },
          "required": [
            "amount",
            "method"
          ]
        },
        "GiftsStyleResponseDto": {
          "type": "object",
          "properties": {
            "slug": {
              "type": "string"
            },
            "name": {
              "type": "string"
            },
            "instruction": {
              "type": "string"
            },
            "isAvailable": {
              "type": "boolean"
            },
            "hue": {
              "type": "number"
            },
            "emojis": {
              "type": "string"
            }
          },
          "required": [
            "slug",
            "name",
            "instruction",
            "isAvailable",
            "hue",
            "emojis"
          ]
        },
        "AvailableGiftsStylesResponseDto": {
          "type": "object",
          "properties": {
            "items": {
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/GiftsStyleResponseDto"
              }
            },
            "total": {
              "type": "number"
            }
          },
          "required": [
            "items",
            "total"
          ]
        },
        "EditGiftRequestDto": {
          "type": "object",
          "properties": {
            "comment": {
              "type": "string"
            }
          }
        },
        "BonusListResponseDto": {
          "type": "object",
          "properties": {
            "id": {
              "type": "number",
              "example": 1,
              "description": "Unique identifier of the bonus"
            },
            "typeSlug": {
              "type": "string",
              "example": "discount",
              "description": "Type slug of the bonus"
            },
            "duration": {
              "type": "number",
              "example": 3600,
              "description": "Duration of the bonus in seconds"
            },
            "expiresAt": {
              "format": "date-time",
              "type": "string",
              "example": "2024-12-31T23:59:59Z",
              "description": "Expiration date of the bonus"
            },
            "isActivated": {
              "type": "boolean",
              "example": false,
              "description": "Whether the bonus is activated"
            },
            "isExpired": {
              "type": "boolean",
              "example": false,
              "description": "Whether the bonus is expired"
            },
            "payload": {
              "type": "object",
              "example": {
                "amount": 100
              },
              "description": "Payload data of the bonus"
            }
          },
          "required": [
            "id",
            "typeSlug",
            "isActivated",
            "isExpired",
            "payload"
          ]
        },
        "BonusesResponseDto": {
          "type": "object",
          "properties": {
            "items": {
              "type": "array",
              "items": {
                "$ref": "#/components/schemas/BonusListResponseDto"
              }
            },
            "total": {
              "type": "number"
            }
          },
          "required": [
            "items",
            "total"
          ]
        },
        "UseGiftcodeResponseDto": {
          "type": "object",
          "properties": {
            "amount": {
              "type": "number",
              "deprecated": true,
              "description": "Legacy field"
            },
            "grantedBonus": {
              "description": "Bonus granted to account after giftcode usage",
              "allOf": [
                {
                  "$ref": "#/components/schemas/BonusListResponseDto"
                }
              ]
            }
          },
          "required": [
            "amount",
            "grantedBonus"
          ]
        },
        "SystemAlertResponseDto": {
          "type": "object",
          "properties": {
            "id": {
              "type": "number",
              "description": "Alert ID"
            },
            "slots": {
              "description": "Alert slot identifiers",
              "type": "array",
              "items": {
                "type": "string"
              }
            },
            "slot": {
              "type": "string",
              "description": "Alert slot identifier (deprecated, use slots instead)",
              "deprecated": true,
              "nullable": true
            },
            "title": {
              "type": "string",
              "description": "Alert title translated to user language",
              "nullable": true
            },
            "body": {
              "type": "string",
              "description": "Alert body content translated to user language"
            },
            "metadata": {
              "type": "object",
              "description": "Additional metadata",
              "nullable": true
            },
            "createdAt": {
              "format": "date-time",
              "type": "string",
              "description": "Alert creation date"
            }
          },
          "required": [
            "id",
            "slots",
            "slot",
            "title",
            "body",
            "metadata",
            "createdAt"
          ]
        },
        "UnsubscribeMailRequestDto": {
          "type": "object",
          "properties": {
            "challenge": {
              "type": "string"
            }
          },
          "required": [
            "challenge"
          ]
        }
      }
    }
  },
  "customOptions": {}
};
  url = options.swaggerUrl || url
  let urls = options.swaggerUrls
  let customOptions = options.customOptions
  let spec1 = options.swaggerDoc
  let swaggerOptions = {
    spec: spec1,
    url: url,
    urls: urls,
    dom_id: '#swagger-ui',
    deepLinking: true,
    presets: [
      SwaggerUIBundle.presets.apis,
      SwaggerUIStandalonePreset
    ],
    plugins: [
      SwaggerUIBundle.plugins.DownloadUrl
    ],
    layout: "StandaloneLayout"
  }
  for (let attrname in customOptions) {
    swaggerOptions[attrname] = customOptions[attrname];
  }
  let ui = SwaggerUIBundle(swaggerOptions)

  if (customOptions.initOAuth) {
    ui.initOAuth(customOptions.initOAuth)
  }

  if (customOptions.authAction) {
    ui.authActions.authorize(customOptions.authAction)
  }
  
  window.ui = ui
}


