use serde::{Deserialize, Serialize};
use std::fs;
use std::path::PathBuf;
use tauri::menu::{Menu, MenuItem, Submenu};
use tauri::{AppHandle, Manager};
use tauri_plugin_opener::OpenerExt;

const DEFAULT_SERVER_URL: &str = "http://127.0.0.1:8080";

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Settings {
    pub server_url: String,
}

impl Default for Settings {
    fn default() -> Self {
        Self {
            server_url: DEFAULT_SERVER_URL.to_string(),
        }
    }
}

fn settings_path(app: &AppHandle) -> Result<PathBuf, String> {
    let dir = app
        .path()
        .app_config_dir()
        .map_err(|e| format!("config dir: {e}"))?;
    Ok(dir.join("settings.json"))
}

fn load_settings(app: &AppHandle) -> Result<Settings, String> {
    let path = settings_path(app)?;
    let raw = match fs::read_to_string(&path) {
        Ok(s) => s,
        Err(err) if err.kind() == std::io::ErrorKind::NotFound => {
            return Ok(Settings::default());
        }
        Err(err) => return Err(format!("read settings: {err}")),
    };
    serde_json::from_str(&raw).map_err(|e| format!("parse settings: {e}"))
}

fn save_settings(app: &AppHandle, settings: &Settings) -> Result<(), String> {
    let path = settings_path(app)?;
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent).map_err(|e| format!("create config dir: {e}"))?;
    }
    let raw =
        serde_json::to_string_pretty(settings).map_err(|e| format!("encode settings: {e}"))?;
    fs::write(&path, raw).map_err(|e| format!("write settings: {e}"))
}

fn normalize_server_url(url: &str) -> Result<String, String> {
    let trimmed = url.trim().trim_end_matches('/').to_string();
    if trimmed.is_empty() {
        return Err("Server URL is required".into());
    }
    if !(trimmed.starts_with("http://") || trimmed.starts_with("https://")) {
        return Err("Server URL must start with http:// or https://".into());
    }
    Ok(trimmed)
}

#[tauri::command]
fn get_settings(app: AppHandle) -> Result<Settings, String> {
    load_settings(&app)
}

#[tauri::command]
fn set_server_url(app: AppHandle, url: String) -> Result<Settings, String> {
    let mut settings = load_settings(&app)?;
    settings.server_url = normalize_server_url(&url)?;
    save_settings(&app, &settings)?;
    Ok(settings)
}

fn build_menu(app: &tauri::App) -> tauri::Result<()> {
    let reload = MenuItem::with_id(app, "reload", "Reload", true, Some("CmdOrCtrl+R"))?;
    let open_server = MenuItem::with_id(app, "open-server", "Open Server URL", true, None::<&str>)?;
    let quit = MenuItem::with_id(app, "quit", "Quit", true, Some("CmdOrCtrl+Q"))?;

    #[cfg(target_os = "macos")]
    {
        let app_menu = Submenu::with_id_and_items(app, "app", "BuildBee", true, &[&quit])?;
        let file_only =
            Submenu::with_id_and_items(app, "file", "File", true, &[&reload, &open_server])?;
        let menu = Menu::with_items(app, &[&app_menu, &file_only])?;
        app.set_menu(menu)?;
    }

    #[cfg(not(target_os = "macos"))]
    {
        let file_menu =
            Submenu::with_id_and_items(app, "file", "File", true, &[&reload, &open_server, &quit])?;
        let menu = Menu::with_items(app, &[&file_menu])?;
        app.set_menu(menu)?;
    }

    Ok(())
}

fn handle_menu(app: &AppHandle, id: &str) {
    match id {
        "reload" => {
            if let Some(window) = app.get_webview_window("main") {
                let _ = window.reload();
            }
        }
        "open-server" => {
            let url = load_settings(app)
                .map(|s| s.server_url)
                .unwrap_or_else(|_| DEFAULT_SERVER_URL.to_string());
            if let Err(err) = app.opener().open_url(url, None::<&str>) {
                eprintln!("open Server URL: {err}");
            }
        }
        "quit" => {
            app.exit(0);
        }
        _ => {}
    }
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_opener::init())
        .invoke_handler(tauri::generate_handler![get_settings, set_server_url])
        .setup(|app| {
            build_menu(app)?;
            Ok(())
        })
        .on_menu_event(|app, event| {
            handle_menu(app, event.id().as_ref());
        })
        .run(tauri::generate_context!())
        .expect("error while running BuildBee");
}

#[cfg(test)]
mod tests {
    use super::normalize_server_url;

    #[test]
    fn rejects_empty_and_non_http() {
        assert!(normalize_server_url("").is_err());
        assert!(normalize_server_url("ftp://x").is_err());
    }

    #[test]
    fn strips_trailing_slash() {
        assert_eq!(
            normalize_server_url("http://127.0.0.1:8080/").unwrap(),
            "http://127.0.0.1:8080"
        );
    }
}
