<?php
header("Cache-Control: s-maxage=5");
header("Date: " . date("D, j M Y G:i:s T"));
echo "Hello, " . $_SERVER['REQUEST_URI'] . "<br>\n";
$dots = str_repeat('.', 200);
echo str_repeat( $dots . '<br>', 40);
