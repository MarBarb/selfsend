#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

fn main() {
    selfsend_windows_lib::run();
}
