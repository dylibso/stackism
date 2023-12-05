use extism_pdk::*;
use serde::{Deserialize, Serialize};

#[derive(Deserialize)]
struct KVWriteOp {
    pub bucket: String,
    pub key: String,
    pub value: String,
}

#[derive(Deserialize)]
struct KVReadOp {
    pub bucket: String,
    pub key: String,
}

#[derive(Serialize)]
struct KVStatus {
    pub status: String,
}

#[host_fn]
extern "ExtismHost" {
    fn kv_read(bucket: String, key: String) -> Vec<u8>;
    fn kv_write(bucket: String, key: String, value: Vec<u8>);
}

#[plugin_fn]
pub fn get_html(_input: Vec<u8>) -> FnResult<String> {
    let output = unsafe {
        let mut data = kv_read("".into(), "a".into())?;
        data.push(1);

        kv_write("".into(), "a".into(), data)?;

        kv_read("".into(), "a".into())?
    };
    Ok(format!("value for key = {:?}", output))
}

#[plugin_fn]
pub fn put_json(Json(input): Json<KVWriteOp>) -> FnResult<Json<KVStatus>> {
    unsafe { kv_write(input.bucket, input.key, input.value.into_bytes())? };

    Ok(Json(KVStatus {
        status: "ok".into(),
    }))
}

#[plugin_fn]
pub fn post_json(Json(input): Json<KVReadOp>) -> FnResult<Vec<u8>> {
    let value = unsafe { kv_read(input.bucket, input.key)? };

    Ok(value)
}

#[plugin_fn]
pub fn get_json(_input: ()) -> FnResult<String> {
    let bucket = config::get("bucket")?.unwrap_or_default();
    let key = config::get("key")?.expect("no key provided");
    let value = unsafe { kv_read(bucket.clone(), key.clone())? };

    info!("bucket = {}", bucket);
    info!("key = {}", key);
    info!("value = {:?} ({} bytes)", value, value.len());
    Ok(format!("{{ \"value\": {:?} }}", value))
}
