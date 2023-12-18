use extism_pdk::*;
use serde::Serialize;

#[derive(Serialize)]
pub struct ImageInfo {
    pub width: String,
    pub height: String,
}

impl Default for ImageInfo {
    fn default() -> Self {
        Self {
            width: "0px".into(),
            height: "0px".into(),
        }
    }
}

#[plugin_fn]
pub fn get_json(_input: Vec<u8>) -> FnResult<Json<ImageInfo>> {
    match dimensions() {
        Ok((w, h)) => Ok(Json(ImageInfo {
            width: w,
            height: h,
        })),
        Err(e) => Err(WithReturnCode::new(e.0, 400)),
    }
}

#[plugin_fn]
pub fn get_html(_input: Vec<u8>) -> FnResult<String> {
    match dimensions() {
        Ok((w, h)) => Ok(format!(
            "<p>width: {w}</p><p>height: {h}</p> <img src='https://github.com/extism.png'/>"
        )),
        Err(e) => Err(WithReturnCode::new(e.0, 400)),
    }
}

#[plugin_fn]
pub fn get_text(_input: Vec<u8>) -> FnResult<String> {
    match dimensions() {
        Ok((w, h)) => Ok(format!("width: {w} ................ height: {h}")),
        Err(e) => Err(WithReturnCode::new(e.0, 400)),
    }
}

#[plugin_fn]
pub fn respond(input: Vec<u8>) -> FnResult<String> {
    Ok(format!(
        "Default response from multi-content plugin... input length = {}",
        input.len()
    ))
}

fn dimensions() -> FnResult<(String, String)> {
    let w = extism_pdk::config::get("width")?.unwrap_or_else(|| "0px".into());
    let h = extism_pdk::config::get("height")?.unwrap_or_else(|| "0px".into());

    Ok((w, h))
}
